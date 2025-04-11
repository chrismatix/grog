package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type WorkspaceConfig struct {
	GrogRoot      string `mapstructure:"grog_root"`
	WorkspaceRoot string `mapstructure:"workspace_root"`
	FailFast      bool   `mapstructure:"fail_fast"`
}

var Global WorkspaceConfig

func (w WorkspaceConfig) GetCacheDirectory() string {
	if w.GrogRoot == "" {
		// This can only ever happen if the user intentionally sets the GrogRoot to ""
		// or if we have an initialization bug.
		// In any case we should exit with an error because otherwise we might end up
		// writing/removing files in the wrong place.
		fmt.Println("GROG_ROOT is not set (or set to \"\").")
		os.Exit(1)
	}
	return filepath.Join(w.GrogRoot, "cache")
}
