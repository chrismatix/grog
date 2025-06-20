package cmd

import (
	"errors"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"grog/internal/cmd/cmds"
	"grog/internal/config"
	"grog/internal/console"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var RootCmd = &cobra.Command{
	Use: "grog",
	// PersistentPreRunE runs before any subcommand's Run, after flags are parsed.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("help") || cmd.Flags().Changed("version") ||
			cmd.Name() == "help" || cmd.Name() == "completion" {
			return nil
		}

		workspaceRoot := config.MustFindWorkspaceRoot()
		viper.Set("workspace_root", workspaceRoot)
		viper.AddConfigPath(workspaceRoot)

		// Initialize config (read file, env, flags)
		if err := initConfig(); err != nil {
			return err
		}

		return config.Global.Validate()
	},
}

// Stamp sets the data for the version command
func Stamp(version string, commit string, buildDate string) {
	RootCmd.Version = version

	RootCmd.SetVersionTemplate(fmt.Sprintf(
		"%s (%s) built on %s",
		version,
		commit,
		buildDate,
	))
}

func init() {
	cobra.OnInitialize()

	RootCmd.InitDefaultCompletionCmd()
	RootCmd.CompletionOptions.DisableDefaultCmd = false

	// Set up Viper
	viper.SetConfigType("toml")
	viper.SetEnvPrefix("GROG")
	viper.AddConfigPath("$HOME/.grog")                     // optionally look for config in the home directory
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_")) // allow FLAG-NAME to map to ENV VAR_NAME
	viper.AutomaticEnv()                                   // read in environment variables that match

	// Set default global root directory
	viper.SetDefault("root", filepath.Join(os.Getenv("HOME"), ".grog"))

	// Options:
	// color
	RootCmd.PersistentFlags().String("color", "auto", "Set color output (yes, no, or auto)")
	err := viper.BindPFlag("color", RootCmd.PersistentFlags().Lookup("color"))
	viper.SetDefault("color", "auto")

	// debug
	RootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	err = viper.BindPFlag("debug", RootCmd.PersistentFlags().Lookup("debug"))
	RootCmd.PersistentFlags().CountP("verbose", "v", "Set verbosity level (-v, -vv)")
	err = viper.BindPFlag("verbose", RootCmd.PersistentFlags().Lookup("verbose"))

	// fail_fast
	RootCmd.PersistentFlags().Bool("fail-fast", false, "Fail fast on first error")
	err = viper.BindPFlag("fail_fast", RootCmd.PersistentFlags().Lookup("fail-fast"))

	// all_platforms
	RootCmd.PersistentFlags().BoolP("all-platforms", "a", false, "Select all platforms (bypasses platform selectors)")
	err = viper.BindPFlag("all_platforms", RootCmd.PersistentFlags().Lookup("all-platforms"))

	// stream_logs
	RootCmd.PersistentFlags().Bool("stream-logs", false, "Forward all target build/test logs to stdout/-err")
	err = viper.BindPFlag("stream_logs", RootCmd.PersistentFlags().Lookup("stream-logs"))

	// load_outputs
	RootCmd.PersistentFlags().String("load-outputs", "all", "Level of output loading for cached targets. One of: all, minimal.")
	err = viper.BindPFlag("load_outputs", RootCmd.PersistentFlags().Lookup("load-outputs"))
	viper.SetDefault("load_outputs", "all")

	// tags
	RootCmd.PersistentFlags().StringSlice("tag", []string{}, "Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar")
	err = viper.BindPFlag("tag", RootCmd.PersistentFlags().Lookup("tag"))
	RootCmd.PersistentFlags().StringSlice("exclude-tag", []string{}, "Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar")
	err = viper.BindPFlag("exclude_tag", RootCmd.PersistentFlags().Lookup("exclude-tag"))

	// enable_caching
	RootCmd.PersistentFlags().Bool("enable-cache", true, "Enable cache")
	err = viper.BindPFlag("enable_cache", RootCmd.PersistentFlags().Lookup("enable-cache"))
	viper.SetDefault("enable_cache", true)

	// select profiles
	RootCmd.PersistentFlags().String("profile", "", "Select a configuration profile to use")
	err = viper.BindPFlag("profile", RootCmd.PersistentFlags().Lookup("profile"))
	viper.SetDefault("profile", "")

	// Register subcommands
	RootCmd.AddCommand(cmds.VersionCmd)
	RootCmd.AddCommand(cmds.ListCmd)
	RootCmd.AddCommand(cmds.InfoCmd)
	RootCmd.AddCommand(cmds.CheckCmd)
	RootCmd.AddCommand(cmds.TaintCmd)
	cmds.AddRunCmd(RootCmd)
	cmds.AddGraphCmd(RootCmd)
	cmds.AddCleanCmd(RootCmd)
	cmds.AddTestCmd(RootCmd)
	cmds.AddBuildCmd(RootCmd)
	cmds.AddDepsCmd(RootCmd)
	cmds.AddRDepsCmd(RootCmd)
	cmds.AddOwnersCmd(RootCmd)
	cmds.AddChangesCmd(RootCmd)
	cmds.AddListCmd(RootCmd)

	if err != nil {
		panic(err)
	}
}

func initConfig() error {
	// Set log_level based on verbosity flag
	switch viper.GetInt("verbose") {
	case 1:
		viper.Set("log_level", "debug")
	case 2:
		viper.Set("log_level", "trace")
	}

	if viper.GetBool("debug") {
		// Set log_level based on debug flag
		viper.Set("log_level", "debug")
	}

	// Set defaults here
	viper.SetDefault("log_level", "info")
	viper.SetDefault("load_outputs", "all")
	viper.SetDefault("disable_non_deterministic_logging", false)
	viper.SetDefault("os", runtime.GOOS)
	viper.SetDefault("arch", runtime.GOARCH)
	viper.SetDefault("load_outputs", "all")
	viper.SetDefault("cache.gcs.shared_cache", true)
	viper.SetDefault("environment_variables", make(map[string]string))

	names := []string{"grog"}
	if os.Getenv("CI") == "1" {
		names = append([]string{"grog.ci"}, names...)
	}
	if viper.GetString("profile") != "" {
		names = append([]string{"grog." + viper.GetString("profile")}, names...)
	}

	var found bool
	for _, name := range names {
		viper.SetConfigName(name)
		if err := viper.ReadInConfig(); err != nil {
			var configFileNotFoundError viper.ConfigFileNotFoundError
			if errors.As(err, &configFileNotFoundError) {
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

	// Merge all config sources into the global
	if err := viper.Unmarshal(&config.Global); err != nil {
		return fmt.Errorf("Failed to parse config: %v\n", err)
	}

	logger := console.InitLogger()
	logger.Debugf("Using config file: %s", viper.ConfigFileUsed())
	logger.Debugf("Running on %s", config.Global.GetPlatform())

	if err := readInEnvironmentVariablesConfig(); err != nil {
		return err
	}

	return nil
}

// Viper always normalizes all configuration keys to be lower-case
// but users should be able to specify upper case environment_variables
// So as a workaround we load the section here a second time _if_ there are env vars
func readInEnvironmentVariablesConfig() error {
	if len(config.Global.EnvironmentVariables) == 0 {
		// nothing to load
		return nil
	}

	raw, err := os.ReadFile(viper.ConfigFileUsed())
	if err != nil {
		return err
	}

	var helper EnvVarsHelper
	err = toml.Unmarshal(raw, &helper)
	if err != nil {
		return err
	}

	config.Global.EnvironmentVariables = helper.EnvironmentVariables
	return nil
}

type EnvVarsHelper struct {
	EnvironmentVariables map[string]string `toml:"environment_variables"`
}
