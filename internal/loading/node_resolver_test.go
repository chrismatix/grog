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
	require.Equal(t, map[string]resolverPackage{
		"packages/theme": {Dependencies: []string{}, Inputs: []string{"package.json", "src/**/*"}},
		"packages/utils": {Dependencies: []string{}, Inputs: []string{"index.js", "package.json"}},
		"packages/ui":    {Dependencies: []string{"packages/theme", "packages/utils"}, Inputs: []string{"package.json", "src/**/*"}},
		"apps/web":       {Dependencies: []string{"packages/theme", "packages/ui"}, Inputs: []string{"package.json", "src/**/*"}},
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
	require.Equal(t, []string{
		"package.json", "pnpm-workspace.yaml", "aube-workspace.yaml", "pnpm-lock.yaml", "package-lock.json", "yarn.lock",
		"apps/web/package.json", "packages/theme/package.json", "packages/ui/package.json", "packages/utils/package.json",
	}, nodeDefaultInputs("testdata/node"))
	require.Len(t, nodeDefaultInputs(t.TempDir()), 6)
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
	packages, operationError := LoadAllPackages(t.Context())
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
