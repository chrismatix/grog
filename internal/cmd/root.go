package cmd

import (
	"fmt"
	"grog/internal/cmd/cmds"
	"grog/internal/cmd/cmds/traces"
	"grog/internal/cmd/flagtypes"
	"grog/internal/config"
	"grog/internal/console"
	"os"
	"path/filepath"
	"runtime"
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
		if cmd.Name() == "lsp" {
			return nil
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

// Stamp sets the data for the version command.
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

var rootConfigured = configureRoot()

func configureRoot() bool {
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
	RootCmd.PersistentFlags().Var(flagtypes.NewEnum("auto", "yes", "no"), "color", "Set color output (yes, no, or auto)")
	_ = viper.BindPFlag("color", RootCmd.PersistentFlags().Lookup("color"))
	viper.SetDefault("color", "auto")

	// debug
	RootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	_ = viper.BindPFlag("debug", RootCmd.PersistentFlags().Lookup("debug"))
	RootCmd.PersistentFlags().CountP("verbose", "v", "Set verbosity level (-v, -vv)")
	_ = viper.BindPFlag("verbose", RootCmd.PersistentFlags().Lookup("verbose"))

	// log_level
	RootCmd.PersistentFlags().Var(flagtypes.NewEnum("", "trace", "debug", "info", "warn", "error"), "log-level", "Set log level (trace, debug, info, warn, error)")
	_ = viper.BindPFlag("log_level", RootCmd.PersistentFlags().Lookup("log-level"))

	// fail_fast
	RootCmd.PersistentFlags().Bool("fail-fast", false, "Fail fast on first error")
	_ = viper.BindPFlag("fail_fast", RootCmd.PersistentFlags().Lookup("fail-fast"))

	// push (used by grog build and grog run)
	RootCmd.PersistentFlags().Bool("push", false, "Push oci:: outputs declared in target.oci_push to their remote destinations after a successful build")
	_ = viper.BindPFlag("push", RootCmd.PersistentFlags().Lookup("push"))

	// skip_workspace_lock
	RootCmd.PersistentFlags().Bool("skip-workspace-lock", false, "Skip the workspace level lock (DANGEROUS: may corrupt the cache)")
	_ = viper.BindPFlag("skip_workspace_lock", RootCmd.PersistentFlags().Lookup("skip-workspace-lock"))

	// all_platforms
	RootCmd.PersistentFlags().BoolP("all-platforms", "a", false, "Select all platforms (bypasses platform selectors)")
	_ = viper.BindPFlag("all_platforms", RootCmd.PersistentFlags().Lookup("all-platforms"))

	// platform
	RootCmd.PersistentFlags().String("platform", "", "Force a specific platform in the form os/arch")
	_ = viper.BindPFlag("platform", RootCmd.PersistentFlags().Lookup("platform"))

	// platform_tag
	RootCmd.PersistentFlags().StringSlice("platform-tag", []string{}, "Enable a custom platform tag for matching targets' platform selectors. Can be used multiple times.")
	_ = viper.BindPFlag("platform_tag", RootCmd.PersistentFlags().Lookup("platform-tag"))

	// stream_logs
	RootCmd.PersistentFlags().Bool("stream-logs", false, "Forward all target build/test logs to stdout/-err")
	_ = viper.BindPFlag("stream_logs", RootCmd.PersistentFlags().Lookup("stream-logs"))

	// output_mode
	RootCmd.PersistentFlags().Var(flagtypes.NewEnum("terse", "detailed"), "output-mode", "Build output style: terse (one line per target) or detailed (stream each target's lifecycle)")
	_ = viper.BindPFlag("output_mode", RootCmd.PersistentFlags().Lookup("output-mode"))
	viper.SetDefault("output_mode", "terse")

	// disable_progress_tracker
	RootCmd.PersistentFlags().Bool("disable-progress-tracker", false, "Disable progress tracking updates")
	_ = viper.BindPFlag("disable_progress_tracker", RootCmd.PersistentFlags().Lookup("disable-progress-tracker"))
	viper.SetDefault("disable_progress_tracker", false)

	// disable_default_shell_flags
	RootCmd.PersistentFlags().Bool("disable-default-shell-flags", false, "Do not prepend \"set -eu\" to target commands")
	_ = viper.BindPFlag("disable_default_shell_flags", RootCmd.PersistentFlags().Lookup("disable-default-shell-flags"))
	viper.SetDefault("disable_default_shell_flags", false)

	// load_outputs
	RootCmd.PersistentFlags().Var(flagtypes.NewEnum("all", "minimal"), "load-outputs", "Level of output loading for cached targets. One of: all, minimal.")
	_ = viper.BindPFlag("load_outputs", RootCmd.PersistentFlags().Lookup("load-outputs"))
	viper.SetDefault("load_outputs", "all")

	// tags
	RootCmd.PersistentFlags().StringSlice("tag", []string{}, "Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar")
	_ = viper.BindPFlag("tag", RootCmd.PersistentFlags().Lookup("tag"))
	RootCmd.PersistentFlags().StringSlice("exclude-tag", []string{}, "Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar")
	_ = viper.BindPFlag("exclude_tag", RootCmd.PersistentFlags().Lookup("exclude-tag"))

	// enable_caching
	RootCmd.PersistentFlags().Bool("enable-cache", true, "Enable cache")
	_ = viper.BindPFlag("enable_cache", RootCmd.PersistentFlags().Lookup("enable-cache"))
	viper.SetDefault("enable_cache", true)

	// select profiles
	RootCmd.PersistentFlags().String("profile", "", "Select a configuration profile to use")
	_ = viper.BindPFlag("profile", RootCmd.PersistentFlags().Lookup("profile"))
	viper.SetDefault("profile", "")

	// async_cache_writes
	RootCmd.PersistentFlags().Bool("async-cache-writes", true, "Defer cache writes to background I/O workers during the build")
	_ = viper.BindPFlag("async_cache_writes", RootCmd.PersistentFlags().Lookup("async-cache-writes"))
	viper.SetDefault("async_cache_writes", true)

	// disable_tea
	RootCmd.PersistentFlags().Bool("disable-tea", false, "Disable interactive TUI (Bubble Tea)")
	_ = viper.BindPFlag("disable_tea", RootCmd.PersistentFlags().Lookup("disable-tea"))
	viper.SetDefault("disable_tea", false)

	// Register subcommands
	RootCmd.AddCommand(cmds.VersionCmd)
	RootCmd.AddCommand(cmds.ListCmd)
	RootCmd.AddCommand(cmds.InfoCmd)
	RootCmd.AddCommand(cmds.CheckCmd)
	RootCmd.AddCommand(cmds.TaintCmd)
	RootCmd.AddCommand(cmds.LSPCmd)
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
	cmds.AddExplainChangesCmd(RootCmd)
	cmds.AddListCmd(RootCmd)
	traces.AddCmd(RootCmd)
	return true
}

func initConfig(cmd *cobra.Command) error {
	// Set defaults here
	viper.SetDefault("log_level", "info")
	viper.SetDefault("load_outputs", "all")
	viper.SetDefault("disable_non_deterministic_logging", false)
	viper.SetDefault("os", runtime.GOOS)
	viper.SetDefault("arch", runtime.GOARCH)
	viper.SetDefault("cache.gcs.shared_cache", true)
	viper.SetDefault("cache.s3.shared_cache", true)
	viper.SetDefault("cache.azure.shared_cache", true)
	viper.SetDefault("hash_algorithm", config.HashAlgorithmXXH3)
	viper.SetDefault("include_hidden", false)
	viper.SetDefault("environment_variables", make(map[string]string))
	viper.SetDefault("traces.enabled", false)

	logger := console.InitLogger()
	if operationError := config.ReadSelectedConfigFromViper(); operationError != nil {
		return operationError
	}
	logger.Debugf("Loaded config file: %s", viper.ConfigFileUsed())

	// Determine effective log level precedence before unmarshalling into Global:
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
	}

	if err := config.LoadGlobalFromViper(); err != nil {
		return err
	}

	logger.Debugf("Using config file: %s", viper.ConfigFileUsed())
	logger.Debugf("Running on %s", config.Global.GetPlatform())

	return nil
}
