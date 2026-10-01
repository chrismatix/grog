package cmds

import (
	"path/filepath"
	"testing"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"

	"github.com/stretchr/testify/require"
)

func TestResolverInputChanged(t *testing.T) {
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	config.Global.WorkspaceRoot = t.TempDir()
	resolver := &model.DependencyResolver{Label: label.TL("tools", "cargo"), Inputs: []string{"Cargo.toml", "crates/a/Cargo.toml"}}
	for _, testCase := range []struct {
		name     string
		changed  string
		expected bool
	}{
		{"input under the declaring package", "tools/crates/a/Cargo.toml", true},
		{"same file name outside the declaring package", "crates/a/Cargo.toml", false},
		{"unrelated file", "tools/src/main.rs", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			changedFiles := []string{filepath.Join(config.Global.WorkspaceRoot, testCase.changed)}
			require.Equal(t, testCase.expected, resolverInputChanged(resolver, changedFiles))
		})
	}
}

func TestResolverTargets(t *testing.T) {
	resolverLabel := label.TL("", "cargo")
	registered := &model.Target{Label: label.TL("crates/a", "sources"), DependencyResolvers: []label.TargetLabel{resolverLabel}}
	synthesized := &model.Target{Label: label.TL("crates/b", "_cargo_package"), DependencyResolvers: []label.TargetLabel{resolverLabel}}
	other := &model.Target{Label: label.TL("crates/c", "build")}
	nodes := model.BuildNodeMapFromNodes(registered, synthesized, other)
	require.ElementsMatch(t, []*model.Target{registered, synthesized}, resolverTargets(nodes, resolverLabel))
}
