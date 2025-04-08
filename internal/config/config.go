package config

type WorkspaceConfig struct {
	FailFast bool `mapstructure:"fail_fast"`
}

var Global WorkspaceConfig
