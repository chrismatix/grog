package main

import (
	"fmt"
	"os"

	"github.com/chrismatix/grog/internal/cmd"
)

// Provisioned by ldflags.
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
