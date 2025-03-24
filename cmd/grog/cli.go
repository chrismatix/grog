package grog

import (
	"grog/internal/cmds"
	"grog/internal/config"
	"grog/internal/console"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use: "grog",
}

func init() {
	// Add commands
	rootCmd.AddCommand(cmds.BuildCmd)
	rootCmd.AddCommand(cmds.TestCmd)
	rootCmd.AddCommand(cmds.CleanCmd)

	// Set up Viper
	viper.SetConfigName("grog")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")           // search in current directory
	viper.AddConfigPath("$HOME/.grog") // optionally look for config in the home directory
	viper.AutomaticEnv()               // read in environment variables that match

	viper.Set("workspace_root", config.MustFindWorkspaceRoot())

	// Set default cache directory
	viper.SetDefault("grog_root", filepath.Join(os.Getenv("HOME"), ".grog"))

	// Add color flag
	rootCmd.PersistentFlags().String("color", "auto", "Set color output (yes, no, or auto)")
	err := viper.BindPFlag("color", rootCmd.PersistentFlags().Lookup("color"))
	if err != nil {
		panic(err)
	}
	viper.SetDefault("color", "auto")

	// Add debug flag
	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	err = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	if err != nil {
		panic(err)
	}

	// Set log_level based on debug flag
	if viper.GetBool("debug") {
		viper.Set("log_level", "debug")
	}

	logger := console.InitLogger()

	// Read in config
	if err = viper.ReadInConfig(); err == nil {
		logger.Debugf("Using config file: %s", viper.ConfigFileUsed())
	}
}

func Run() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
