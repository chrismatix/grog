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
		// TODO Validate the loaded config
		//if err := validateConfig(); err != nil {
		//	return err
		//}
		return nil
	},
}

// Stamp sets the data for the version command
func Stamp(version string, commit string, buildDate string) {
	RootCmd.Version = version

	RootCmd.SetVersionTemplate(fmt.Sprintf(
		"Grog version %s (%s) built on %s",
		version,
		commit,
		buildDate,
	))
}

func init() {
	cobra.OnInitialize()

	// Set up Viper
	viper.SetConfigName("grog")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")                               // search in current directory
	viper.AddConfigPath("$HOME/.grog")                     // optionally look for config in the home directory
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_")) // allow FLAG-NAME to map to ENV VAR_NAME
	viper.AutomaticEnv()                                   // read in environment variables that match

	viper.Set("workspace_root", config.MustFindWorkspaceRoot())

	// Set default cache directory
	viper.SetDefault("grog_root", filepath.Join(os.Getenv("HOME"), ".grog"))

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

	// enable_caching
	RootCmd.PersistentFlags().Bool("enable-caching", true, "Enable caching")
	err = viper.BindPFlag("enable_caching", RootCmd.PersistentFlags().Lookup("enable-caching"))
	viper.SetDefault("enable_caching", true)

	// Register subcommands
	RootCmd.AddCommand(cmds.BuildCmd)
	RootCmd.AddCommand(cmds.TestCmd)
	RootCmd.AddCommand(cmds.GetCleanCmd())
	RootCmd.AddCommand(cmds.VersionCmd)
	RootCmd.AddCommand(cmds.GraphCmd)

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

	// Read config
	logger := console.InitLogger()

	// Read in config
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	logger.Debugf("Using config file: %s", viper.ConfigFileUsed())

	// Merge all config sources into struct
	if err := viper.Unmarshal(&config.Global); err != nil {
		return fmt.Errorf("Failed to parse config: %v\n", err)
	}

	return nil
}
