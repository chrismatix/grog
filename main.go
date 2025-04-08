package main

import (
	"fmt"
	"grog/internal/cmd"
	"os"
)

// Provisioned by ldflags
var (
	version   string
	commit    string
	buildDate string
)

func main() {
	cmd.Stamp(version, commit, buildDate)
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
