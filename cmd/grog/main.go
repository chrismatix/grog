package main

import (
	"fmt"
	"grog/pkg/cmds"
	"grog/pkg/config"
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

	// Read in config
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
