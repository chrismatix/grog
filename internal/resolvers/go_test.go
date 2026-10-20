package resolvers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoDependencies(t *testing.T) {
	document, operationError := goDependencies(t.Context(), "testdata/go")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	rootFingerprint := document.Packages["cmd/app"].Fingerprint["module"]
	require.NotEmpty(t, rootFingerprint)
	require.Equal(t, rootFingerprint, document.Packages[".hidden/pkg"].Fingerprint["module"])
	require.NotEqual(t, rootFingerprint, document.Packages["_deps/local/pkg"].Fingerprint["module"])
	require.NotEqual(t, rootFingerprint, document.Packages["tools/gen"].Fingerprint["module"])
	for directory, reportedPackage := range document.Packages {
		reportedPackage.Fingerprint = nil
		document.Packages[directory] = reportedPackage
	}
	member := func(inputs []string, dependencies ...string) Package {
		return Package{Dependencies: append([]string{}, dependencies...), Inputs: inputs}
	}
	plain := []string{"*", "testdata/**"}
	require.Equal(t, map[string]Package{
		".hidden/pkg":               member(plain, "internal/greet"),
		"cmd/app":                   member(plain, ".hidden/pkg", "_deps/local/pkg", "_deps/local/workspaceonly", "internal/_shim", "internal/greet", "tools/gen"),
		"internal/_shim":            member(plain),
		"internal/greet":            member(plain, "internal/format", "internal/winutil"),
		"internal/format":           member([]string{"*", "banner.txt", "static/**", "templates/*.txt", "templates/*.txt/**", "testdata/**", "vendor/assets/logo.txt"}),
		"internal/winutil":          member([]string{"*", "fixtures/**", "testdata/**"}),
		"_deps/local/pkg":           member(plain),
		"_deps/local/workspaceonly": member(plain),
		"tools/gen":                 member(plain),
	}, document.Packages)
}

func TestGoImportDirectories(t *testing.T) {
	modules := []goModule{{Path: "example.com/mono/tools/gen", Directory: "tools/gen"}, {Path: "example.com/mono", Directory: ""}, {Path: "old.example/lib", Directory: "local"}}
	for _, testCase := range []struct {
		importPath  string
		directories []string
	}{
		{importPath: "example.com/mono", directories: []string{""}},
		{importPath: "example.com/mono/internal/greet", directories: []string{"internal/greet"}},
		{importPath: "example.com/mono/tools/gen/cmd", directories: []string{"tools/gen/cmd"}},
		{importPath: "old.example/lib/pkg", directories: []string{"local/pkg"}},
		{importPath: "example.com/monorepo/x"},
		{importPath: "fmt"},
	} {
		t.Run(testCase.importPath, func(t *testing.T) {
			require.Equal(t, testCase.directories, goImportDirectories(testCase.importPath, modules))
		})
	}
}

func TestGoEmbedPatterns(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		source   string
		expected []string
	}{
		{name: "bare", source: "//go:embed templates/*.txt", expected: []string{"templates/*.txt"}},
		{name: "quoted and raw", source: "//go:embed \"a b.txt\" `static` plain", expected: []string{"a b.txt", "static", "plain"}},
		{name: "all prefix", source: "//go:embed all:assets", expected: []string{"assets"}},
		{name: "not a directive", source: "//go:embedded x", expected: nil},
		{name: "empty", source: "//go:embed", expected: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, goEmbedPatterns(testCase.source))
		})
	}
}

func TestGoDefaultInputs(t *testing.T) {
	inputs, excludeInputs := goDefaultInputs("testdata/go")
	require.Equal(t, []string{"go.work", "go.work.sum", "**/go.mod", "**/go.sum", "**/*.go"}, inputs)
	require.Equal(t, []string{"**/vendor/**", "**/node_modules/**"}, excludeInputs)
}

func TestGoResolverErrors(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		files         map[string]string
		expectedError string
	}{
		{name: "no module", files: map[string]string{"main.go": "package main\n"}, expectedError: "no go.mod under"},
		{name: "module without path", files: map[string]string{"go.mod": "go 1.24\n"}, expectedError: "no module directive"},
		{name: "syntax error", files: map[string]string{"go.mod": "module example.com/x\n", "main.go": "package main\nimport (\n"}, expectedError: "parse"},
		{name: "ignored files are not parsed", files: map[string]string{"go.mod": "module example.com/x\n", "_scratch.go": "not go", ".hidden.go": "not go"}, expectedError: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			for name, contents := range testCase.files {
				require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0644))
			}
			_, operationError := goDependencies(t.Context(), directory)
			if testCase.expectedError == "" {
				require.NoError(t, operationError)
				return
			}
			require.ErrorContains(t, operationError, testCase.expectedError)
		})
	}
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError := goDependencies(cancelledContext, "testdata/go")
	require.ErrorIs(t, operationError, context.Canceled)
}
