package cmd

import (
	"fmt"
	"grog/internal/cmd/cmds"
	"grog/internal/cmd/cmds/traces"
	"grog/internal/config"
	"grog/internal/console"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var Version string

var RootCmd = &cobra.Command{
	Use: "grog",
	// PersistentPreRunE runs before any subcommand's Run, after flags are parsed.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("help") || cmd.Flags().Changed("version") ||
			cmd.Name() == "help" || isCompletionCmd(cmd) {
			return nil
		}

		workspaceRoot := config.MustFindWorkspaceRoot()
		viper.Set("workspace_root", workspaceRoot)
		viper.AddConfigPath(workspaceRoot)

		// Initialize config (read file, env, flags)
		if err := initConfig(cmd); err != nil {
			return err
		}

		if err := config.Global.Validate(); err != nil {
			return err
		}

		if !console.UseTea() {
			config.Global.DisableProgressTracker = true
		}

		if err := config.Global.ValidateGrogVersion(Version); err != nil {
			console.InitLogger().Fatalf("Invalid grog version: %v", err)
		}
		return nil
	},
}

// isCompletionCmd reports whether cmd is the `completion` command or one of
// its subcommands (bash, zsh, fish, powershell).
func isCompletionCmd(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "completion" {
			return true
		}
	}
	return false
}

// Stamp sets the data for the version command
func Stamp(version string, commit string, buildDate string) {
	RootCmd.Version = version
	Version = version
	cmds.GrogVersion = version

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

	// Shared with internal/config.InitForEmbedding so the CLI and the embedded
	// session can't drift on viper setup or default values.
	config.RegisterViperBase()
	config.RegisterDefaults()

	// Options:
	// color
	RootCmd.PersistentFlags().String("color", "auto", "Set color output (yes, no, or auto)")
	err := viper.BindPFlag("color", RootCmd.PersistentFlags().Lookup("color"))

	// debug
	RootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	err = viper.BindPFlag("debug", RootCmd.PersistentFlags().Lookup("debug"))
	RootCmd.PersistentFlags().CountP("verbose", "v", "Set verbosity level (-v, -vv)")
	err = viper.BindPFlag("verbose", RootCmd.PersistentFlags().Lookup("verbose"))

	// log_level
	RootCmd.PersistentFlags().String("log-level", "", "Set log level (trace, debug, info, warn, error)")
	err = viper.BindPFlag("log_level", RootCmd.PersistentFlags().Lookup("log-level"))

	// fail_fast
	RootCmd.PersistentFlags().Bool("fail-fast", false, "Fail fast on first error")
	err = viper.BindPFlag("fail_fast", RootCmd.PersistentFlags().Lookup("fail-fast"))

	// skip_workspace_lock
	RootCmd.PersistentFlags().Bool("skip-workspace-lock", false, "Skip the workspace level lock (DANGEROUS: may corrupt the cache)")
	err = viper.BindPFlag("skip_workspace_lock", RootCmd.PersistentFlags().Lookup("skip-workspace-lock"))

	// all_platforms
	RootCmd.PersistentFlags().BoolP("all-platforms", "a", false, "Select all platforms (bypasses platform selectors)")
	err = viper.BindPFlag("all_platforms", RootCmd.PersistentFlags().Lookup("all-platforms"))

	// platform
	RootCmd.PersistentFlags().String("platform", "", "Force a specific platform in the form os/arch")
	err = viper.BindPFlag("platform", RootCmd.PersistentFlags().Lookup("platform"))

	// platform_tag
	RootCmd.PersistentFlags().StringSlice("platform-tag", []string{}, "Enable a custom platform tag for matching targets' platform selectors. Can be used multiple times.")
	err = viper.BindPFlag("platform_tag", RootCmd.PersistentFlags().Lookup("platform-tag"))

	// stream_logs
	RootCmd.PersistentFlags().Bool("stream-logs", false, "Forward all target build/test logs to stdout/-err")
	err = viper.BindPFlag("stream_logs", RootCmd.PersistentFlags().Lookup("stream-logs"))

	// output_mode
	RootCmd.PersistentFlags().String("output-mode", "terse", "Build output style: terse (one line per target) or detailed (stream each target's lifecycle)")
	err = viper.BindPFlag("output_mode", RootCmd.PersistentFlags().Lookup("output-mode"))

	// disable_progress_tracker
	RootCmd.PersistentFlags().Bool("disable-progress-tracker", false, "Disable progress tracking updates")
	err = viper.BindPFlag("disable_progress_tracker", RootCmd.PersistentFlags().Lookup("disable-progress-tracker"))

	// disable_default_shell_flags
	RootCmd.PersistentFlags().Bool("disable-default-shell-flags", false, "Do not prepend \"set -eu\" to target commands")
	err = viper.BindPFlag("disable_default_shell_flags", RootCmd.PersistentFlags().Lookup("disable-default-shell-flags"))

	// load_outputs
	RootCmd.PersistentFlags().String("load-outputs", "all", "Level of output loading for cached targets. One of: all, minimal.")
	err = viper.BindPFlag("load_outputs", RootCmd.PersistentFlags().Lookup("load-outputs"))

	// tags
	RootCmd.PersistentFlags().StringSlice("tag", []string{}, "Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar")
	err = viper.BindPFlag("tag", RootCmd.PersistentFlags().Lookup("tag"))
	RootCmd.PersistentFlags().StringSlice("exclude-tag", []string{}, "Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar")
	err = viper.BindPFlag("exclude_tag", RootCmd.PersistentFlags().Lookup("exclude-tag"))

	// enable_caching
	RootCmd.PersistentFlags().Bool("enable-cache", true, "Enable cache")
	err = viper.BindPFlag("enable_cache", RootCmd.PersistentFlags().Lookup("enable-cache"))

	// select profiles
	RootCmd.PersistentFlags().String("profile", "", "Select a configuration profile to use")
	err = viper.BindPFlag("profile", RootCmd.PersistentFlags().Lookup("profile"))

	// async_cache_writes
	RootCmd.PersistentFlags().Bool("async-cache-writes", true, "Defer cache writes to background I/O workers during the build")
	err = viper.BindPFlag("async_cache_writes", RootCmd.PersistentFlags().Lookup("async-cache-writes"))

	// disable_tea
	RootCmd.PersistentFlags().Bool("disable-tea", false, "Disable interactive TUI (Bubble Tea)")
	err = viper.BindPFlag("disable_tea", RootCmd.PersistentFlags().Lookup("disable-tea"))

	// Register subcommands
	RootCmd.AddCommand(cmds.VersionCmd)
	RootCmd.AddCommand(cmds.ListCmd)
	RootCmd.AddCommand(cmds.InfoCmd)
	RootCmd.AddCommand(cmds.CheckCmd)
	RootCmd.AddCommand(cmds.TaintCmd)
	cmds.AddRunCmd(RootCmd)
	cmds.AddGraphCmd(RootCmd)
	cmds.AddCleanCmd(RootCmd)
	cmds.AddBuildAndTestCmd(RootCmd)
	cmds.AddTestCmd(RootCmd)
	cmds.AddBuildCmd(RootCmd)
	cmds.AddDepsCmd(RootCmd)
	cmds.AddRDepsCmd(RootCmd)
	cmds.AddOwnersCmd(RootCmd)
	cmds.AddChangesCmd(RootCmd)
	cmds.AddListCmd(RootCmd)
	traces.AddCmd(RootCmd)

	if err != nil {
		panic(err)
	}
}

func initConfig(cmd *cobra.Command) error {
	logger := console.InitLogger()

	if err := config.LoadConfigFile(viper.GetString("profile")); err != nil {
		return err
	}
	logger.Debugf("Loaded config file: %s", viper.ConfigFileUsed())

	// Determine effective log level precedence after loading the config file
	// but before consumers read it:
	// 1) --log-level flag (if set)
	// 2) --verbose/-v or --debug flags
	// 3) workspace config (already read) or env or defaults
	logLevelFlagSet := false
	if cmd != nil {
		if f := cmd.Flags().Lookup("log-level"); f != nil {
			logLevelFlagSet = f.Changed
		}
	}
	if !logLevelFlagSet {
		switch viper.GetInt("verbose") {
		case 1:
			viper.Set("log_level", "debug")
		case 2:
			viper.Set("log_level", "trace")
		}
		if viper.GetBool("debug") {
			viper.Set("log_level", "debug")
		}
		// LoadConfigFile already unmarshalled; sync the log level we just set.
		config.Global.LogLevel = viper.GetString("log_level")
	}

	logger.Debugf("Running on %s", config.Global.GetPlatform())

	platform := viper.GetString("platform")
	if config.Global.AllPlatforms && platform != "" {
		return fmt.Errorf("--platform cannot be used with --all-platforms")
	}
	if platform != "" {
		parts := strings.SplitN(platform, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid platform %s, expected os/arch", platform)
		}
		config.Global.OS = parts[0]
		config.Global.Arch = parts[1]
	}

	return config.ReadEnvironmentVariables()
}
