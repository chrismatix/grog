package loading

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"

	"github.com/stretchr/testify/require"
)

func TestPnpmDependenciesWithoutLockfile(t *testing.T) {
	document, operationError := pnpmPackageManager.dependencies(t.Context(), "testdata/node")
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

func writeNodeWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	for name, contents := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(directory, name)), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0644))
	}
	return directory
}

func TestNodeDependencyEdges(t *testing.T) {
	members := map[string]string{
		"packages/compiler/package.json": `{"name": "@visia/typescript-compiler"}`,
		"packages/theme/package.json":    `{"name": "@demo/theme"}`,
		"packages/utils/package.json":    `{"name": "@demo/utils"}`,
	}
	withMembers := func(files map[string]string) map[string]string {
		maps.Copy(files, members)
		return files
	}
	pnpmWorkspace := "packages:\n  - packages/*\n"
	npmRoot := `{"name": "root", "workspaces": ["packages/*"]}`
	for _, testCase := range []struct {
		name           string
		packageManager nodePackageManager
		files          map[string]string
		expected       []string
	}{
		{
			name:           "workspace alias",
			packageManager: pnpmPackageManager,
			files: withMembers(map[string]string{
				"pnpm-workspace.yaml":       pnpmWorkspace,
				"package.json":              `{"name": "root"}`,
				"packages/app/package.json": `{"name": "app", "devDependencies": {"typescript": "workspace:@visia/typescript-compiler@*"}}`,
			}),
			expected: []string{"packages/compiler"},
		},
		{
			name:           "npm alias",
			packageManager: npmPackageManager,
			files: withMembers(map[string]string{
				"package.json":              npmRoot,
				"packages/app/package.json": `{"name": "app", "dependencies": {"utils": "npm:@demo/utils@^1", "theme": "npm:@demo/theme", "react": "npm:react@19"}}`,
			}),
			expected: []string{"packages/theme", "packages/utils"},
		},
		{
			name:           "link and file specs",
			packageManager: npmPackageManager,
			files: withMembers(map[string]string{
				"package.json":              npmRoot,
				"packages/app/package.json": `{"name": "app", "dependencies": {"theme": "link:../theme", "utils": "file:../utils", "outside": "link:../../../elsewhere"}}`,
			}),
			expected: []string{"packages/theme", "packages/utils"},
		},
		{
			name:           "pnpm override in the lockfile",
			packageManager: pnpmPackageManager,
			files: withMembers(map[string]string{
				"pnpm-workspace.yaml":       pnpmWorkspace + "overrides:\n  left-pad: link:packages/utils\n",
				"package.json":              `{"name": "root"}`,
				"packages/app/package.json": `{"name": "app", "dependencies": {"left-pad": "^1.3.0"}}`,
				"pnpm-lock.yaml": `lockfileVersion: '9.0'
overrides:
  left-pad: link:packages/utils
importers:
  .: {}
  packages/app:
    dependencies:
      left-pad:
        specifier: link:../utils
        version: link:../utils
  packages/utils: {}
`,
			}),
			expected: []string{"packages/utils"},
		},
		{
			name:           "aube lockfile",
			packageManager: aubePackageManager,
			files: withMembers(map[string]string{
				"aube-workspace.yaml":       pnpmWorkspace,
				"package.json":              `{"name": "root"}`,
				"packages/app/package.json": `{"name": "app", "dependencies": {"left-pad": "^1.3.0"}}`,
				"aube-lock.yaml":            "importers:\n  packages/app:\n    dependencies:\n      left-pad:\n        specifier: ^1.3.0\n        version: link:../theme\n",
			}),
			expected: []string{"packages/theme"},
		},
		{
			name:           "only the pnpm lockfile counts",
			packageManager: pnpmPackageManager,
			files: withMembers(map[string]string{
				"pnpm-workspace.yaml":       pnpmWorkspace,
				"package.json":              `{"name": "root", "workspaces": ["packages/*"]}`,
				"packages/app/package.json": `{"name": "app", "dependencies": {"ui-kit": "^2"}}`,
				"pnpm-lock.yaml":            "importers:\n  packages/app:\n    dependencies:\n      ui-kit:\n        specifier: ^2\n        version: link:../theme\n",
				"yarn.lock":                 "\"ui-kit@^2\":\n  version \"0.0.0-use.local\"\n  resolution \"ui-kit@workspace:packages/utils\"\n",
				"package-lock.json":         `{"packages": {"packages/app/node_modules/ui-kit": {"resolved": "packages/compiler", "link": true}}}`,
			}),
			expected: []string{"packages/theme"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document, operationError := testCase.packageManager.dependencies(t.Context(), writeNodeWorkspace(t, testCase.files))
			require.NoError(t, operationError)
			require.Equal(t, testCase.expected, document.Packages["packages/app"].Dependencies)
		})
	}
}

func TestNodeDependencyName(t *testing.T) {
	for _, testCase := range []struct {
		key       string
		specifier string
		expected  string
	}{
		{"typescript", "workspace:@visia/typescript-compiler@*", "@visia/typescript-compiler"},
		{"utils", "workspace:utils@^1", "utils"},
		{"@demo/ui", "workspace:*", "@demo/ui"},
		{"@demo/ui", "workspace:^", "@demo/ui"},
		{"lodash", "npm:@demo/utils@^1.0.0", "@demo/utils"},
		{"lodash", "npm:@demo/utils", "@demo/utils"},
		{"lodash", "npm:lodash-es@4", "lodash-es"},
		{"react", "^19", "react"},
	} {
		t.Run(testCase.specifier, func(t *testing.T) {
			require.Equal(t, testCase.expected, nodeDependencyName(testCase.key, testCase.specifier))
		})
	}
}

func TestNodeMemberPatterns(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		packageManager nodePackageManager
		files          map[string]string
		expected       []string
		expectedError  string
	}{
		{name: "pnpm", packageManager: pnpmPackageManager, files: map[string]string{"pnpm-workspace.yaml": "packages: [libs/*]\n", "package.json": `{"workspaces": ["packages/*"]}`}, expected: []string{"libs/*"}},
		{name: "aube", packageManager: aubePackageManager, files: map[string]string{"aube-workspace.yaml": "packages:\n  - packages/*\n", "pnpm-workspace.yaml": "packages: [libs/*]\n"}, expected: []string{"packages/*"}},
		{name: "pnpm without workspace file", packageManager: pnpmPackageManager, files: map[string]string{"package.json": `{"workspaces": ["packages/*"]}`}, expectedError: "no pnpm-workspace.yaml in"},
		{name: "npm ignores pnpm workspace", packageManager: npmPackageManager, files: map[string]string{"pnpm-workspace.yaml": "packages: [libs/*]\n", "package.json": `{"workspaces": ["packages/*", "apps/web"]}`}, expected: []string{"packages/*", "apps/web"}},
		{name: "yarn object", packageManager: npmPackageManager, files: map[string]string{"package.json": `{"workspaces": {"packages": ["packages/*"], "nohoist": ["**/jest"]}}`}, expected: []string{"packages/*"}},
		{name: "no workspaces", packageManager: npmPackageManager, files: map[string]string{"package.json": `{"name": "single"}`}, expectedError: "no workspaces in"},
		{name: "no package.json", packageManager: npmPackageManager, files: map[string]string{}, expectedError: "read"},
		{name: "invalid yaml", packageManager: pnpmPackageManager, files: map[string]string{"pnpm-workspace.yaml": "packages: [\n"}, expectedError: "parse"},
		{name: "invalid workspaces", packageManager: npmPackageManager, files: map[string]string{"package.json": `{"workspaces": 42}`}, expectedError: "parse workspaces"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			patterns, operationError := testCase.packageManager.memberPatterns(writeNodeWorkspace(t, testCase.files))
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
	inputs, excludeInputs := pnpmPackageManager.defaultInputs("testdata/node")
	require.Equal(t, []string{"package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml", "packages/*/package.json", "apps/**/package.json"}, inputs)
	require.Equal(t, []string{"**/node_modules/**", "**/excluded/**"}, excludeInputs)
	inputs, excludeInputs = pnpmPackageManager.defaultInputs(t.TempDir())
	require.Equal(t, []string{"package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml"}, inputs)
	require.Equal(t, []string{"**/node_modules/**"}, excludeInputs)
	inputs, _ = npmPackageManager.defaultInputs(writeNodeWorkspace(t, map[string]string{"package.json": `{"workspaces": ["packages/*"]}`}))
	require.Equal(t, []string{"package.json", "packages/*/package.json"}, inputs)
}

func TestNodeResolverErrors(t *testing.T) {
	directory := writeNodeWorkspace(t, map[string]string{
		"pnpm-workspace.yaml":          "packages: [packages/*]\n",
		"packages/broken/package.json": "{",
	})
	_, operationError := pnpmPackageManager.dependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "parse")
	directory = writeNodeWorkspace(t, map[string]string{
		"pnpm-workspace.yaml": "packages: [packages/*]\n",
		"package.json":        "{}",
		"pnpm-lock.yaml":      "importers: [\n",
	})
	_, operationError = pnpmPackageManager.dependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "parse "+filepath.Join(directory, "pnpm-lock.yaml"))
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError = pnpmPackageManager.dependencies(cancelledContext, "testdata/node")
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
	require.Nil(t, nodes[label.TL("next.js", "_npm_package")])
}
