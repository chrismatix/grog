package resolvers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUvDependencies(t *testing.T) {
	document, operationError := uvDependencies(t.Context(), "testdata/uv")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	// Members reaching pytest or the external path dependency carry a
	// fingerprint; TestUvThirdPartyFingerprint covers what goes into it.
	for packagePath, reportedPackage := range document.Packages {
		require.Equal(t, packagePath == "server" || packagePath == "cli", reportedPackage.Fingerprint["lock"] != "", packagePath)
		reportedPackage.Fingerprint = nil
		document.Packages[packagePath] = reportedPackage
	}
	excludeInputs := []string{"**/.venv/**", "**/__pycache__/**"}
	require.Equal(t, map[string]Package{
		"lib/format":   {Dependencies: []string{}, Inputs: []string{"format/**/*", "pyproject.toml"}, ExcludeInputs: excludeInputs},
		"lib/proto":    {Dependencies: []string{}, Inputs: []string{"**/*.py", "**/*.pyi", "demo_proto/**/*", "pyproject.toml", "src/demo_proto/**/*"}, ExcludeInputs: excludeInputs},
		"server":       {Dependencies: []string{"lib/format", "lib/proto", "tools/deploy"}, Inputs: []string{"pyproject.toml", "src/server/**/*"}, ExcludeInputs: excludeInputs},
		"cli":          {Dependencies: []string{"lib/format", "server"}, Inputs: []string{"**/*.py", "**/*.pyi", "cli/**/*", "pyproject.toml", "src/cli/**/*"}, ExcludeInputs: excludeInputs},
		"tools/deploy": {Dependencies: []string{}, Inputs: []string{"**/*.py", "**/*.pyi", "deploy/**/*", "pyproject.toml", "src/deploy/**/*"}, ExcludeInputs: excludeInputs},
		"lib/multi":    {Dependencies: []string{}, Inputs: []string{"pyproject.toml", "src/one/**/*", "src/two/inner/**/*"}, ExcludeInputs: excludeInputs},
	}, document.Packages)
	require.NotContains(t, document.Packages, "../external")
}

const uvFingerprintLock = `version = 1

[manifest]
members = ["a", "b", "c"]

[[package]]
name = "a"
version = "0.1.0"
source = { editable = "a" }
dependencies = [{ name = "requests" }]

[[package]]
name = "b"
version = "0.1.0"
source = { editable = "b" }
dependencies = [{ name = "numpy" }]

[[package]]
name = "c"
version = "0.1.0"
source = { editable = "c" }
dependencies = [{ name = "a" }]

[package.dev-dependencies]
dev = [{ name = "pytest" }]

[[package]]
name = "requests"
version = "2.32.0"
source = { registry = "https://pypi.org/simple" }
dependencies = [{ name = "urllib3" }]

[[package]]
name = "urllib3"
version = "2.0.0"
source = { registry = "https://pypi.org/simple" }
sdist = { url = "https://example.com/urllib3-2.0.0.tar.gz", hash = "sha256:sdist" }

[[package]]
name = "numpy"
version = "1.26.0"
source = { registry = "https://pypi.org/simple" }
wheels = [{ url = "https://example.com/numpy-1.26.0.whl", hash = "sha256:wheel" }]

[[package]]
name = "pytest"
version = "8.0.0"
source = { registry = "https://pypi.org/simple" }
`

func TestUvThirdPartyFingerprint(t *testing.T) {
	resolve := func(t *testing.T, lock string) Document {
		t.Helper()
		directory := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(directory, "uv.lock"), []byte(lock), 0644))
		document, operationError := uvDependencies(t.Context(), directory)
		require.NoError(t, operationError)
		return document
	}
	base := resolve(t, uvFingerprintLock)
	require.Equal(t, []string{"a"}, base.Packages["c"].Dependencies)
	for _, testCase := range []struct {
		name     string
		old      string
		new      string
		expected []string
	}{
		{"transitive bump stops at the member boundary", `version = "2.0.0"`, `version = "2.0.1"`, []string{"a"}},
		{"artifact hash change", "sha256:wheel", "sha256:rebuilt", []string{"b"}},
		{"dev group bump", `version = "8.0.0"`, `version = "8.1.0"`, []string{"c"}},
		{"resolution fork adds a second entry", "[[package]]\nname = \"pytest\"", "[[package]]\nname = \"numpy\"\nversion = \"2.0.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n\n[[package]]\nname = \"pytest\"", []string{"b"}},
		{"unreferenced package", "[[package]]\nname = \"pytest\"", "[[package]]\nname = \"black\"\nversion = \"24.0.0\"\n\n[[package]]\nname = \"pytest\"", nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			lock := strings.Replace(uvFingerprintLock, testCase.old, testCase.new, 1)
			require.NotEqual(t, uvFingerprintLock, lock)
			document := resolve(t, lock)
			var changed []string
			for _, member := range []string{"a", "b", "c"} {
				require.NotEmpty(t, document.Packages[member].Fingerprint["lock"])
				require.Equal(t, base.Packages[member].Dependencies, document.Packages[member].Dependencies)
				if document.Packages[member].Fingerprint["lock"] != base.Packages[member].Fingerprint["lock"] {
					changed = append(changed, member)
				}
			}
			require.Equal(t, testCase.expected, changed)
		})
	}
}

func TestUvDefaultInputs(t *testing.T) {
	inputs, excludeInputs := uvDefaultInputs(t.Context(), "testdata/uv")
	require.Equal(t, []string{
		"cli/pyproject.toml", "lib/format/pyproject.toml", "lib/multi/pyproject.toml", "lib/proto/pyproject.toml",
		"pyproject.toml", "server/pyproject.toml", "tools/deploy/pyproject.toml", "uv.lock",
	}, inputs)
	require.Equal(t, []string{"**/.venv/**", "**/__pycache__/**"}, excludeInputs)
	inputs, _ = uvDefaultInputs(t.Context(), t.TempDir())
	require.Equal(t, []string{"uv.lock", "pyproject.toml"}, inputs)
}

func TestUvResolverErrors(t *testing.T) {
	directory := t.TempDir()
	_, operationError := uvDependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "read uv lock")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "uv.lock"), []byte("[[package"), 0644))
	_, operationError = uvDependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "parse uv lock")
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError = uvDependencies(cancelledContext, "testdata/uv")
	require.ErrorIs(t, operationError, context.Canceled)
}
