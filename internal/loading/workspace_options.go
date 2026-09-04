package loading

import (
	"os"
	"path/filepath"

	"grog/internal/config"

	"github.com/pelletier/go-toml/v2"
)

// WorkspaceIncludesHidden returns the effective package discovery setting.
func WorkspaceIncludesHidden(workspaceRoot string) bool {
	if config.Global.WorkspaceRoot != "" && filepath.Clean(config.Global.WorkspaceRoot) == filepath.Clean(workspaceRoot) {
		return config.Global.IncludeHidden
	}
	configurationPath, operationError := config.SelectedWorkspaceConfigPath(workspaceRoot)
	if operationError != nil || configurationPath == "" {
		return false
	}
	configurationBytes, operationError := os.ReadFile(configurationPath)
	if operationError != nil {
		return false
	}
	var configuration struct {
		IncludeHidden bool `toml:"include_hidden"`
	}
	if toml.Unmarshal(configurationBytes, &configuration) != nil {
		return false
	}
	return configuration.IncludeHidden
}
