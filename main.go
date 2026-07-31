package main

import (
	"grog/internal/cmd"
	"grog/internal/console"
	"os"
)

// Provisioned by ldflags.
var (
	version   string
	commit    string
	buildDate string
)

func main() {
	cmd.Stamp(version, commit, buildDate)
	if console.ConfigureJSONFromArguments(os.Args[1:]) {
		cmd.RootCmd.SilenceErrors = true
		cmd.RootCmd.SilenceUsage = true
	}
	if err := cmd.RootCmd.Execute(); err != nil {
		if console.JSONEnabled() {
			console.WriteError(console.ErrorCodeInvalidInvocation, err.Error())
		} else {
			console.WriteText(err.Error() + "\n")
		}
		os.Exit(2)
	}
}
