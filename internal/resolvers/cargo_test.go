package resolvers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCargoDependencies(t *testing.T) {
	document, operationError := cargoDependencies(t.Context(), "testdata/cargo")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	require.Len(t, document.Packages, 7)
	require.NotContains(t, document.Packages, "crates/excluded")
	for _, testCase := range []struct {
		name         string
		packagePath  string
		dependencies []string
	}{
		{"normal build and conditional dependencies", "crates/app", []string{"crates/generator", "crates/platform", "crates/shared"}},
		{"transitive dependency", "crates/shared", []string{"crates/leaf"}},
		{"dev dependency cycle excluded", "crates/dev", []string{"crates/app"}},
		{"glob member leaf", "crates/leaf", []string{}},
		{"glob member generator", "crates/generator", []string{}},
		{"glob member platform", "crates/platform", []string{}},
		{"inherited workspace dependency, excluded path ignored", "crates/inherit", []string{"crates/shared"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Contains(t, document.Packages, testCase.packagePath)
			require.Equal(t, testCase.dependencies, document.Packages[testCase.packagePath].Dependencies)
			require.Subset(t, document.Packages[testCase.packagePath].Inputs, []string{"Cargo.toml", "build.rs", "src/**/*", "tests/**/*"})
			require.Equal(t, []string{"target/**"}, document.Packages[testCase.packagePath].ExcludeInputs)
		})
	}
	require.Contains(t, document.Packages["crates/platform"].Inputs, "lib.rs")
}

func TestCargoDefaultInputs(t *testing.T) {
	inputs, excludeInputs := cargoDefaultInputs(t.Context(), "testdata/cargo")
	require.Equal(t, []string{"Cargo.toml", "Cargo.lock", "crates/*/Cargo.toml"}, inputs)
	require.Equal(t, []string{"target/**"}, excludeInputs)
	inputs, _ = cargoDefaultInputs(t.Context(), t.TempDir())
	require.Equal(t, []string{"Cargo.toml", "Cargo.lock"}, inputs)
}

func TestCargoResolverErrors(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		root          string
		member        string
		expectedError string
	}{
		{"missing workspace manifest", "", "", "read cargo manifest"},
		{"malformed workspace manifest", "[workspace", "", "parse cargo manifest"},
		{"malformed member glob", "[workspace]\nmembers = ['[']", "", "invalid cargo workspace member glob"},
		{"missing member manifest", "[workspace]\nmembers = ['member']", "", "read cargo manifest"},
		{"malformed member manifest", "[workspace]\nmembers = ['member']", "[package", "parse cargo manifest"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(directory, "member"), 0755))
			if testCase.root != "" {
				require.NoError(t, os.WriteFile(filepath.Join(directory, "Cargo.toml"), []byte(testCase.root), 0644))
			}
			if testCase.member != "" {
				require.NoError(t, os.WriteFile(filepath.Join(directory, "member/Cargo.toml"), []byte(testCase.member), 0644))
			}
			_, operationError := cargoDependencies(t.Context(), directory)
			require.ErrorContains(t, operationError, testCase.expectedError)
		})
	}
}

func TestCargoResolverRootMemberAndReadOnly(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.CopyFS(directory, os.DirFS("testdata/cargo")))
	// The root package is a member by having [package], without listing '.'.
	rootManifest := []byte("[workspace]\nmembers = ['crates/*']\n[package]\nname = 'root'\nversion = '0.1.0'\n[dependencies]\napp = { path = 'crates/app' }\n")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "Cargo.toml"), rootManifest, 0444))
	lockFile := filepath.Join(directory, "Cargo.lock")
	require.NoError(t, os.WriteFile(lockFile, []byte("unchanged lockfile\n"), 0444))
	t.Setenv("PATH", "")
	document, operationError := cargoDependencies(t.Context(), directory)
	require.NoError(t, operationError)
	require.Equal(t, []string{"crates/app"}, document.Packages[""].Dependencies)
	contents, operationError := os.ReadFile(lockFile)
	require.NoError(t, operationError)
	require.Equal(t, "unchanged lockfile\n", string(contents))
	contents, operationError = os.ReadFile(filepath.Join(directory, "Cargo.toml"))
	require.NoError(t, operationError)
	require.Equal(t, rootManifest, contents)
	loadContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError = cargoDependencies(loadContext, directory)
	require.ErrorIs(t, operationError, context.Canceled)
}

func TestCargoResolverRustExample(t *testing.T) {
	document, operationError := cargoDependencies(t.Context(), "../../examples/rust_monorepo")
	require.NoError(t, operationError)
	dependencies := make(map[string][]string, len(document.Packages))
	for packagePath, reportedPackage := range document.Packages {
		dependencies[packagePath] = reportedPackage.Dependencies
		require.Subset(t, reportedPackage.Inputs, []string{"Cargo.toml", "src/**/*"}, packagePath)
	}
	require.Equal(t, map[string][]string{
		"crates/cli":    {"crates/greet"},
		"crates/format": {},
		"crates/greet":  {"crates/format"},
		"crates/server": {"crates/greet"},
	}, dependencies)
}
