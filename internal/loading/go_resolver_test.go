package loading

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
	member := func(inputs []string, dependencies ...string) resolverPackage {
		return resolverPackage{Dependencies: append([]string{}, dependencies...), Inputs: inputs, ExcludeInputs: []string{"vendor/**"}}
	}
	plain := []string{"*", "testdata/**"}
	require.Equal(t, map[string]resolverPackage{
		"cmd/app":          member(plain, "internal/greet", "tools/gen"),
		"internal/greet":   member(plain, "internal/format", "internal/winutil"),
		"internal/format":  member([]string{"*", "banner.txt", "banner.txt/**", "static", "static/**", "templates/*.txt", "templates/*.txt/**", "testdata/**"}),
		"internal/winutil": member(plain),
		"tools/gen":        member(plain),
	}, document.Packages)
}

func TestGoImportDirectory(t *testing.T) {
	modules := []goModule{{Path: "example.com/mono/tools/gen", Directory: "tools/gen"}, {Path: "example.com/mono", Directory: ""}}
	for _, testCase := range []struct {
		importPath string
		directory  string
		isLocal    bool
	}{
		{importPath: "example.com/mono", directory: "", isLocal: true},
		{importPath: "example.com/mono/internal/greet", directory: "internal/greet", isLocal: true},
		{importPath: "example.com/mono/tools/gen/cmd", directory: "tools/gen/cmd", isLocal: true},
		{importPath: "example.com/monorepo/x", isLocal: false},
		{importPath: "fmt", isLocal: false},
	} {
		t.Run(testCase.importPath, func(t *testing.T) {
			directory, isLocal := goImportDirectory(testCase.importPath, modules)
			require.Equal(t, testCase.isLocal, isLocal)
			require.Equal(t, testCase.directory, directory)
		})
	}
}

func TestGoEmbedPatterns(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		source   string
		expected []string
	}{
		{name: "bare", source: "//go:embed templates/*.txt\nvar x embed.FS\n", expected: []string{"templates/*.txt"}},
		{name: "quoted and raw", source: "  //go:embed \"a b.txt\" `static` plain\n", expected: []string{"a b.txt", "static", "plain"}},
		{name: "not a directive", source: "//go:embedded\n// go:embed x\n//go:embed\n", expected: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, goEmbedPatterns([]byte(testCase.source)))
		})
	}
}

func TestGoDefaultInputs(t *testing.T) {
	inputs, excludeInputs := goDefaultInputs("testdata/go")
	require.Equal(t, []string{"go.work", "go.work.sum", "**/go.mod", "**/go.sum", "**/*.go"}, inputs)
	require.Equal(t, []string{"**/vendor/**", "**/testdata/**", "**/node_modules/**", "**/.*/**", "**/_*/**"}, excludeInputs)
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
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			for name, contents := range testCase.files {
				require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0644))
			}
			_, operationError := goDependencies(t.Context(), directory)
			require.ErrorContains(t, operationError, testCase.expectedError)
		})
	}
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError := goDependencies(cancelledContext, "testdata/go")
	require.ErrorIs(t, operationError, context.Canceled)
}
