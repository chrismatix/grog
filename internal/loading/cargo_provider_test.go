package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCargoDependencies(t *testing.T) {
	document, err := cargoDependencies(t.Context(), "testdata/cargo")
	require.NoError(t, err)
	require.Equal(t, 1, document.Version)
	require.Len(t, document.Packages, 6)
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
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Contains(t, document.Packages, testCase.packagePath)
			require.Equal(t, testCase.dependencies, document.Packages[testCase.packagePath].Dependencies)
		})
	}
}

func TestCargoProviderErrors(t *testing.T) {
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
			_, err := cargoDependencies(t.Context(), directory)
			require.ErrorContains(t, err, testCase.expectedError)
		})
	}
}

func TestCargoProviderRootMemberAndReadOnly(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.CopyFS(directory, os.DirFS("testdata/cargo")))
	rootManifest, err := os.ReadFile(filepath.Join(directory, "Cargo.toml"))
	require.NoError(t, err)
	rootManifest = []byte("[workspace]\nmembers = ['.', 'crates/*']\n[package]\nname = 'root'\nversion = '0.1.0'\n[dependencies]\napp = { path = 'crates/app' }\n")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "Cargo.toml"), rootManifest, 0444))
	lockFile := filepath.Join(directory, "Cargo.lock")
	require.NoError(t, os.WriteFile(lockFile, []byte("unchanged lockfile\n"), 0444))
	t.Setenv("PATH", "")
	document, err := cargoDependencies(t.Context(), directory)
	require.NoError(t, err)
	require.Equal(t, []string{"crates/app"}, document.Packages[""].Dependencies)
	contents, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	require.Equal(t, "unchanged lockfile\n", string(contents))
	contents, err = os.ReadFile(filepath.Join(directory, "Cargo.toml"))
	require.NoError(t, err)
	require.Equal(t, rootManifest, contents)
	loadContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = cargoDependencies(loadContext, directory)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCargoProviderRustExample(t *testing.T) {
	document, err := cargoDependencies(t.Context(), "../../examples/rust_monorepo")
	require.NoError(t, err)
	require.Equal(t, map[string]providerPackage{
		"crates/cli":    {Dependencies: []string{"crates/greet"}},
		"crates/format": {Dependencies: []string{}},
		"crates/greet":  {Dependencies: []string{"crates/format"}},
		"crates/server": {Dependencies: []string{"crates/greet"}},
	}, document.Packages)
}
