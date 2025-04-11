package config

type WorkspaceConfig struct {
	GrogRoot      string `mapstructure:"grog_root"`
	WorkspaceRoot string `mapstructure:"workspace_root"`
	FailFast      bool   `mapstructure:"fail_fast"`
}

var Global WorkspaceConfig
