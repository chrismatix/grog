package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
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
// It mirrors the CLI's PersistentPreRunE/initConfig wiring from internal/cmd,
// minus cobra flag binding, and forces headless rendering settings so no TUI is
// started. Because grog config lives in the global viper instance and
// config.Global, this configures a single workspace per process.
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

	// Viper setup (mirrors internal/cmd.init()).
	viper.SetConfigType("toml")
	viper.SetEnvPrefix("GROG")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.AddConfigPath(filepath.Join(os.Getenv("HOME"), ".grog"))
	viper.AddConfigPath(workspaceRoot)

	viper.Set("workspace_root", workspaceRoot)

	// Defaults (mirror internal/cmd.init() and initConfig()).
	viper.SetDefault("root", filepath.Join(os.Getenv("HOME"), ".grog"))
	viper.SetDefault("color", "no")
	viper.SetDefault("output_mode", "terse")
	viper.SetDefault("disable_default_shell_flags", false)
	viper.SetDefault("enable_cache", true)
	viper.SetDefault("async_cache_writes", true)
	viper.SetDefault("log_level", "info")
	viper.SetDefault("load_outputs", "all")
	viper.SetDefault("disable_non_deterministic_logging", false)
	viper.SetDefault("os", runtime.GOOS)
	viper.SetDefault("arch", runtime.GOARCH)
	viper.SetDefault("cache.gcs.shared_cache", true)
	viper.SetDefault("cache.s3.shared_cache", true)
	viper.SetDefault("cache.azure.shared_cache", true)
	viper.SetDefault("hash_algorithm", HashAlgorithmXXH3)
	viper.SetDefault("include_hidden", false)
	viper.SetDefault("environment_variables", make(map[string]string))
	viper.SetDefault("traces.enabled", false)

	// Always headless when embedded: never start the Bubble Tea TUI and never
	// render progress bars (the host process owns stdout).
	viper.Set("disable_tea", true)
	viper.Set("disable_progress_tracker", true)

	names := []string{"grog"}
	if os.Getenv("CI") == "1" {
		names = append([]string{"grog.ci"}, names...)
	}
	if opts.Profile != "" {
		names = append([]string{"grog." + opts.Profile}, names...)
	}

	var found bool
	for _, name := range names {
		viper.SetConfigName(name)
		if readErr := viper.ReadInConfig(); readErr != nil {
			var notFound viper.ConfigFileNotFoundError
			if errors.As(readErr, &notFound) {
				continue
			}
			return readErr
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("no grog config file found in %q (tried: %v)", workspaceRoot, names)
	}

	if err := viper.Unmarshal(&Global); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	Global.HashAlgorithm = strings.ToLower(Global.HashAlgorithm)

	if err := readEnvironmentVariablesConfig(); err != nil {
		return err
	}

	return Global.Validate()
}

// readEnvironmentVariablesConfig mirrors internal/cmd.readInEnvironmentVariablesConfig:
// viper lower-cases keys, so inline environment_variables and any
// environment_variables_file are re-read here preserving case, with inline
// values taking precedence over file values.
func readEnvironmentVariablesConfig() error {
	merged := make(map[string]string)

	if Global.EnvironmentVariablesFile != "" {
		envFilePath := Global.EnvironmentVariablesFile
		if !filepath.IsAbs(envFilePath) {
			envFilePath = filepath.Join(Global.WorkspaceRoot, envFilePath)
		}
		f, err := os.Open(envFilePath)
		if err != nil {
			return fmt.Errorf("failed to open environment_variables_file %q: %w", envFilePath, err)
		}
		defer f.Close()
		for k, v := range gotenv.Parse(f) {
			merged[k] = v
		}
	}

	if len(Global.EnvironmentVariables) > 0 {
		raw, err := os.ReadFile(viper.ConfigFileUsed())
		if err != nil {
			return err
		}
		var helper struct {
			EnvironmentVariables map[string]string `toml:"environment_variables"`
		}
		if err := toml.Unmarshal(raw, &helper); err != nil {
			return err
		}
		for k, v := range helper.EnvironmentVariables {
			merged[k] = v
		}
	}

	Global.EnvironmentVariables = merged
	return nil
}
