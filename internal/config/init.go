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

// RegisterViperBase configures the global viper instance with the static bits
// every grog entry point needs: config file format, env-var prefix, env-key
// replacer, the $HOME/.grog config path, and AutomaticEnv. Idempotent.
//
// Both the CLI (internal/cmd.init) and the embedded session
// (InitForEmbedding) call this so they cannot drift apart.
func RegisterViperBase() {
	viper.SetConfigType("toml")
	viper.SetEnvPrefix("GROG")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.AddConfigPath(filepath.Join(os.Getenv("HOME"), ".grog"))
}

// RegisterDefaults sets every viper default shared by the CLI and the embedded
// session. The CLI's flag definitions still own their default values (passed to
// pflag), but those bindings only take effect once the flag is read; viper
// defaults provide the fallback for config-file/env consumption. Idempotent.
func RegisterDefaults() {
	viper.SetDefault("root", filepath.Join(os.Getenv("HOME"), ".grog"))

	// Mirrors the flag defaults registered in internal/cmd.init so that any
	// code path reading viper before flags are parsed sees the same value.
	viper.SetDefault("color", "auto")
	viper.SetDefault("output_mode", "terse")
	viper.SetDefault("disable_progress_tracker", false)
	viper.SetDefault("disable_default_shell_flags", false)
	viper.SetDefault("load_outputs", "all")
	viper.SetDefault("enable_cache", true)
	viper.SetDefault("profile", "")
	viper.SetDefault("async_cache_writes", true)
	viper.SetDefault("disable_tea", false)

	// Mirrors the defaults previously set in internal/cmd.initConfig.
	viper.SetDefault("log_level", "info")
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
}

// LoadConfigFile resolves grog.toml (plus the CI and profile overlays in
// precedence order) from the configured viper search path, then unmarshals it
// into Global. Returns an error if no config file is found.
//
// Lowercases HashAlgorithm afterwards so case-insensitive comparisons are safe.
func LoadConfigFile(profile string) error {
	names := []string{"grog"}
	if os.Getenv("CI") == "1" {
		names = append([]string{"grog.ci"}, names...)
	}
	if profile != "" {
		names = append([]string{"grog." + profile}, names...)
	}

	var found bool
	for _, name := range names {
		viper.SetConfigName(name)
		if err := viper.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if errors.As(err, &notFound) {
				continue
			}
			return err
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("no grog config file found (tried: %v)", names)
	}

	if err := viper.Unmarshal(&Global); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	Global.HashAlgorithm = strings.ToLower(Global.HashAlgorithm)
	return nil
}

// envVarsHelper is the minimal toml shape used to recover the original
// (case-preserving) environment_variables map from grog.toml; viper would
// otherwise lowercase the keys.
type envVarsHelper struct {
	EnvironmentVariables map[string]string `toml:"environment_variables"`
}

// ReadEnvironmentVariables merges any environment_variables_file with the
// inline environment_variables table from grog.toml, preserving key case
// (which viper would otherwise lowercase). Inline values override file values.
// Populates Global.EnvironmentVariables.
func ReadEnvironmentVariables() error {
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
		var helper envVarsHelper
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
