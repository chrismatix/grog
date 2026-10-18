package cmds

import (
	"path/filepath"
	"testing"

	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/label"
	"github.com/chrismatix/grog/internal/model"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentChangedFiles(t *testing.T) {
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	config.Global.WorkspaceRoot = t.TempDir()
	environment := &model.Environment{
		Label:          label.TL("envs", "linux"),
		SourceFilePath: filepath.Join(config.Global.WorkspaceRoot, "envs", "BUILD.yaml"),
		Inputs:         []string{"provider.sh"},
	}
	for _, testCase := range []struct {
		name     string
		changed  string
		expected bool
	}{
		{"provider input", "envs/provider.sh", true},
		{"environment definition", "envs/BUILD.yaml", true},
		{"same file name outside the package", "provider.sh", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			changedFiles := []string{filepath.Join(config.Global.WorkspaceRoot, testCase.changed)}
			require.Equal(t, testCase.expected, len(environmentChangedFiles(environment, changedFiles)) > 0)
		})
	}
}

func TestEnvironmentTargets(t *testing.T) {
	environmentLabel := label.TL("envs", "linux")
	sandboxed := &model.Target{Label: label.TL("app", "build"), Environment: &environmentLabel}
	onHost := &model.Target{Label: label.TL("app", "lint")}
	nodes := model.BuildNodeMapFromNodes(sandboxed, onHost)
	require.Equal(t, []*model.Target{sandboxed}, environmentTargets(nodes, environmentLabel))
}
