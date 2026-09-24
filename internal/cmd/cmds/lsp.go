package cmds

import (
	"os"

	"grog/internal/lsp"

	"github.com/spf13/cobra"
)

// LSPCmd starts the grog language server.
var LSPCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Start the grog language server.",
	Long:  "Start the grog language server over standard input and output for Starlark and YAML build files.",
	Args:  cobra.NoArgs,
	RunE: func(command *cobra.Command, arguments []string) error {
		return lsp.Serve(command.Context(), os.Stdin, os.Stdout)
	},
}
