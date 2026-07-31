package cmds

import (
	"grog/internal/mcpserver"

	"github.com/spf13/cobra"
)

var MCPCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serves Grog query tools over the Model Context Protocol.",
	Args:  cobra.NoArgs,
	RunE: func(command *cobra.Command, arguments []string) error {
		return mcpserver.Run(command.Context(), GrogVersion)
	},
}

func AddMCPCmd(rootCommand *cobra.Command) {
	rootCommand.AddCommand(MCPCmd)
}
