package cmds

import (
	"grog/internal/lsp"
	"os"

	"github.com/spf13/cobra"
)

// LspCmd starts the grog language server.
var LspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Start the grog language server",
	Long:  "Start the grog language server for BUILD.star, BUILD.yaml, and BUILD.yml files.",
	RunE: func(command *cobra.Command, arguments []string) error {
		return lsp.Serve(command.Context(), os.Stdin, os.Stdout)
	},
}
