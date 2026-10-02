package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"

	"github.com/stretchr/testify/require"
)

func TestNodeDependencies(t *testing.T) {
	document, operationError := nodeDependencies(t.Context(), "testdata/node")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	member := func(dependencies ...string) resolverPackage {
		return resolverPackage{Dependencies: append([]string{}, dependencies...), Inputs: []string{"**/*"}, ExcludeInputs: []string{"**/node_modules/**"}}
	}
	require.Equal(t, map[string]resolverPackage{
		"packages/theme": member(),
		"packages/utils": member(),
		"packages/ui":    member("packages/theme", "packages/utils"),
		"apps/web":       member("packages/theme", "packages/ui", "packages/utils"),
		"":               {Dependencies: []string{"packages/utils"}, Inputs: []string{"package.json"}, ExcludeInputs: []string{"**/node_modules/**"}},
	}, document.Packages)
}

func TestNodeMemberPatterns(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		files         map[string]string
		expected      []string
		expectedError string
	}{
		{name: "aube workspace", files: map[string]string{"aube-workspace.yaml": "packages:\n  - packages/*\n"}, expected: []string{"packages/*"}},
		{name: "pnpm wins over package.json", files: map[string]string{"pnpm-workspace.yaml": "packages: [libs/*]\n", "package.json": `{"workspaces": ["packages/*"]}`}, expected: []string{"libs/*"}},
		{name: "npm list", files: map[string]string{"package.json": `{"workspaces": ["packages/*", "apps/web"]}`}, expected: []string{"packages/*", "apps/web"}},
		{name: "yarn object", files: map[string]string{"package.json": `{"workspaces": {"packages": ["packages/*"], "nohoist": ["**/jest"]}}`}, expected: []string{"packages/*"}},
		{name: "no workspace", files: map[string]string{"package.json": `{"name": "single"}`}, expectedError: "no workspace in"},
		{name: "no package.json", files: map[string]string{}, expectedError: "read"},
		{name: "invalid yaml", files: map[string]string{"pnpm-workspace.yaml": "packages: [\n"}, expectedError: "parse"},
		{name: "invalid workspaces", files: map[string]string{"package.json": `{"workspaces": 42}`}, expectedError: "parse workspaces"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			for name, contents := range testCase.files {
				require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0644))
			}
			patterns, operationError := nodeMemberPatterns(directory)
			if testCase.expectedError != "" {
				require.ErrorContains(t, operationError, testCase.expectedError)
				return
			}
			require.NoError(t, operationError)
			require.Equal(t, testCase.expected, patterns)
		})
	}
}

func TestNodeDefaultInputs(t *testing.T) {
	inputs, excludeInputs := nodeDefaultInputs("testdata/node")
	require.Equal(t, []string{"package.json", "pnpm-workspace.yaml", "aube-workspace.yaml", "packages/*/package.json", "apps/**/package.json"}, inputs)
	require.Equal(t, []string{"**/node_modules/**", "**/excluded/**"}, excludeInputs)
	inputs, excludeInputs = nodeDefaultInputs(t.TempDir())
	require.Equal(t, []string{"package.json", "pnpm-workspace.yaml", "aube-workspace.yaml"}, inputs)
	require.Equal(t, []string{"**/node_modules/**"}, excludeInputs)
}

func TestNodeResolverErrors(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "pnpm-workspace.yaml"), []byte("packages: [packages/*]\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "packages/broken"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "packages/broken/package.json"), []byte("{"), 0644))
	_, operationError := nodeDependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "parse")
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError = nodeDependencies(cancelledContext, "testdata/node")
	require.ErrorIs(t, operationError, context.Canceled)
}

func TestJsExampleInfersPackageDependencies(t *testing.T) {
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	workspaceDirectory, operationError := filepath.Abs("../../examples/js")
	require.NoError(t, operationError)
	config.Global.WorkspaceRoot = workspaceDirectory
	packages, operationError := LoadAllPackages(t.Context(), &DependencyInferrer{})
	require.NoError(t, operationError)
	nodes, operationError := model.BuildNodeMapFromPackages(packages)
	require.NoError(t, operationError)
	build, isTarget := nodes[label.TL("next.js", "build")].(*model.Target)
	require.True(t, isTarget)
	require.Equal(t, []label.TargetLabel{
		label.TL("", "install"), label.TL("packages/theme", "build"), label.TL("packages/ui-components", "build"), label.TL("packages/utils", "build"),
	}, build.Dependencies)
	require.Nil(t, nodes[label.TL("next.js", "_node_package")])
}
