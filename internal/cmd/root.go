package cmd

import (
	"fmt"
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
		// Initialize config (read file, env, flags)
		if err := initConfig(); err != nil {
			return err
		}

		if err := config.Global.Validate(); err != nil {
			return err
		}
		return nil
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

	// Find the current workspace root
	workspaceRoot := config.MustFindWorkspaceRoot()
	viper.Set("workspace_root", workspaceRoot)

	// Set up Viper
	viper.SetConfigName("grog")
	viper.SetConfigType("toml")
	viper.SetEnvPrefix("GROG")
	viper.AddConfigPath(workspaceRoot)                     // search in workspace root
	viper.AddConfigPath("$HOME/.grog")                     // optionally look for config in the home directory
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_")) // allow FLAG-NAME to map to ENV VAR_NAME
	viper.AutomaticEnv()                                   // read in environment variables that match

	// Set default cache directory
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

	// tags
	RootCmd.PersistentFlags().StringSlice("tag", []string{}, "Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar")
	err = viper.BindPFlag("tag", RootCmd.PersistentFlags().Lookup("tag"))
	RootCmd.PersistentFlags().StringSlice("exclude-tag", []string{}, "Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar")
	err = viper.BindPFlag("exclude_tag", RootCmd.PersistentFlags().Lookup("exclude-tag"))

	// enable_caching
	RootCmd.PersistentFlags().Bool("enable-cache", true, "Enable cache")
	err = viper.BindPFlag("enable_cache", RootCmd.PersistentFlags().Lookup("enable-cache"))
	viper.SetDefault("enable_cache", true)

	// Register subcommands
	RootCmd.AddCommand(cmds.BuildCmd)
	RootCmd.AddCommand(cmds.TestCmd)
	RootCmd.AddCommand(cmds.RunCmd)
	RootCmd.AddCommand(cmds.GetCleanCmd())
	RootCmd.AddCommand(cmds.VersionCmd)
	RootCmd.AddCommand(cmds.GraphCmd)
	RootCmd.AddCommand(cmds.ListCmd)
	RootCmd.AddCommand(cmds.InfoCmd)
	RootCmd.AddCommand(cmds.CheckCmd)
	cmds.AddDepsCmd(RootCmd)
	cmds.AddRDepsCmd(RootCmd)
	cmds.AddOwnersCmd(RootCmd)
	cmds.AddChangesCmd(RootCmd)

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
	viper.SetDefault("fail_fast", false)
	viper.SetDefault("log_level", "info")
	viper.SetDefault("os", runtime.GOOS)
	viper.SetDefault("arch", runtime.GOARCH)

	// Read in config
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	// Merge all config sources into struct
	if err := viper.Unmarshal(&config.Global); err != nil {
		return fmt.Errorf("Failed to parse config: %v\n", err)
	}

	// Read config
	logger := console.InitLogger()
	logger.Debugf("Using config file: %s", viper.ConfigFileUsed())
	logger.Debugf("Running on %s", config.Global.GetPlatform())

	return nil
}
