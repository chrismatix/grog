package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// EmbeddingOptions tunes how InitForEmbedding loads configuration.
type EmbeddingOptions struct {
	// WorkspaceRoot is the directory containing grog.toml. Required.
	WorkspaceRoot string
	// Profile optionally selects a grog.<profile>.toml overlay (like --profile).
	Profile string
}

// InitForEmbedding populates the package-level Global from a workspace's
// grog.toml, environment, and defaults, for callers that embed grog as a
// library rather than running the CLI (e.g. the Terraform provider).
//
// It shares its setup (RegisterViperBase, RegisterDefaults, LoadConfigFile,
// ReadEnvironmentVariables) with internal/cmd so the two paths can't drift.
// Forces headless rendering settings so no TUI is started. Because grog
// configuration lives in the global viper and config.Global, this configures a
// single workspace per process.
func InitForEmbedding(opts EmbeddingOptions) error {
	if opts.WorkspaceRoot == "" {
		return errors.New("WorkspaceRoot is required")
	}
	workspaceRoot, err := filepath.Abs(opts.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("resolving workspace root: %w", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, "grog.toml")); err != nil {
		return fmt.Errorf("no grog.toml in workspace root %q: %w", workspaceRoot, err)
	}

	// Start from a clean global viper so repeated initializations (and tests)
	// don't accumulate config paths or stale keys from a prior workspace.
	viper.Reset()
	RegisterViperBase()
	RegisterDefaults()

	// The workspace's grog.toml is the canonical source; surface it ahead of
	// $HOME/.grog so per-workspace settings win.
	viper.AddConfigPath(workspaceRoot)
	viper.Set("workspace_root", workspaceRoot)

	// Always headless when embedded: never start the Bubble Tea TUI, never
	// render progress bars, and never forward target output to stdout (the host
	// process owns it — e.g. go-plugin's gRPC protocol under the Terraform
	// provider).
	viper.Set("disable_tea", true)
	viper.Set("disable_progress_tracker", true)
	viper.Set("stream_logs", false)
	// Default to plain output too — color escape codes in a captured log
	// writer (e.g. tflog) are noise.
	viper.SetDefault("color", "no")

	if err := LoadConfigFile(opts.Profile); err != nil {
		return err
	}
	if err := ReadEnvironmentVariables(); err != nil {
		return err
	}
	return Global.Validate()
}
