package loading

import (
	"path/filepath"
	"testing"

	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/label"
	"github.com/chrismatix/grog/internal/model"

	"github.com/stretchr/testify/require"
)

func TestRustExampleSynthesizesCrateFilegroups(t *testing.T) {
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	workspaceDirectory, operationError := filepath.Abs("../../examples/rust_monorepo")
	require.NoError(t, operationError)
	config.Global.WorkspaceRoot = workspaceDirectory
	packages, operationError := LoadAllPackages(t.Context(), &DependencyInferrer{})
	require.NoError(t, operationError)
	nodes, operationError := model.BuildNodeMapFromPackages(packages)
	require.NoError(t, operationError)
	for _, testCase := range []struct {
		crate      string
		dependency string
	}{
		{"cli", "greet"}, {"format", ""}, {"greet", "format"}, {"server", "greet"},
	} {
		t.Run(testCase.crate, func(t *testing.T) {
			packagePath := "crates/" + testCase.crate
			filegroup, isTarget := nodes[label.TL(packagePath, "_cargo_package")].(*model.Target)
			require.True(t, isTarget)
			require.Empty(t, filegroup.Command)
			require.Equal(t, filepath.Join(workspaceDirectory, "BUILD.yaml"), filegroup.SourceFilePath)
			require.Contains(t, filegroup.Inputs, "Cargo.toml")
			require.Contains(t, filegroup.UnresolvedInputs, "src/**/*")
			var expectedDependencies []label.TargetLabel
			if testCase.dependency != "" {
				expectedDependencies = []label.TargetLabel{label.TL("crates/"+testCase.dependency, "_cargo_package")}
			}
			require.Equal(t, expectedDependencies, filegroup.Dependencies)
			for _, target := range nodes.GetTargets() {
				if target.Label.Package != packagePath || target == filegroup {
					continue
				}
				require.Contains(t, target.Dependencies, filegroup.Label)
			}
		})
	}
}

func TestCustomResolverExample(t *testing.T) {
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	workspaceDirectory, operationError := filepath.Abs("../../examples/custom_resolver")
	require.NoError(t, operationError)
	config.Global.WorkspaceRoot = workspaceDirectory
	packages, operationError := LoadAllPackages(t.Context(), &DependencyInferrer{})
	require.NoError(t, operationError)
	nodes, operationError := model.BuildNodeMapFromPackages(packages)
	require.NoError(t, operationError)
	for packagePath, dependency := range map[string]string{"proto/base": "", "proto/user": "proto/base", "proto/order": "proto/user"} {
		filegroup, isTarget := nodes[label.TL(packagePath, "_protos_package")].(*model.Target)
		require.True(t, isTarget, packagePath)
		require.Equal(t, []string{packagePath[len("proto/"):] + ".proto"}, filegroup.Inputs)
		if dependency == "" {
			require.Empty(t, filegroup.Dependencies)
			continue
		}
		require.Equal(t, []label.TargetLabel{label.TL(dependency, "_protos_package")}, filegroup.Dependencies)
	}
	require.Nil(t, nodes[label.TL("proto/base", "generate")])
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
