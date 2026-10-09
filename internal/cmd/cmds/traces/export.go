package traces

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismatix/grog/internal/cmd/flagtypes"
	"github.com/chrismatix/grog/internal/console"
	"github.com/chrismatix/grog/internal/tracing"
)

var (
	exportFormat = flagtypes.NewEnum("jsonl", "otel")
	exportOutput string
	exportLimit  int
	exportSince  string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export traces for dashboard integration.",
	Example: `  grog traces export --format=jsonl
  grog traces export --format=otel --output traces.json`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getStore(ctx, logger)
		defer store.Close()

		opts := tracing.ListOptions{Limit: exportLimit}
		if exportSince != "" {
			sinceTime, err := time.Parse("2006-01-02", exportSince)
			if err != nil {
				logger.Fatalf("invalid --since date: %v", err)
			}
			opts.Since = &sinceTime
		}

		entries, err := store.List(ctx, opts)
		if err != nil {
			logger.Fatalf("failed to list traces: %v", err)
		}

		if len(entries) == 0 {
			logger.Info("No traces to export.")
			return
		}

		var w io.Writer = os.Stdout
		var outputFile *os.File
		if exportOutput != "" {
			var openErr error
			outputFile, openErr = os.Create(exportOutput)
			if openErr != nil {
				logger.Fatalf("failed to create output file: %v", openErr)
			}
			w = outputFile
		}

		switch exportFormat.Value {
		case "jsonl":
			if err := tracing.ExportJSONL(ctx, store, entries, w); err != nil {
				logger.Fatalf("export failed: %v", err)
			}
		case "otel":
			if err := tracing.ExportOTLP(ctx, store, entries, w); err != nil {
				logger.Fatalf("export failed: %v", err)
			}
		}

		if outputFile != nil {
			if err := outputFile.Close(); err != nil {
				logger.Fatalf("failed to close output file: %v", err)
			}
		}
	},
}

func registerExportCmd() {
	exportCmd.Flags().Var(exportFormat, "format", "Export format: jsonl or otel")
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "Output file (default: stdout)")
	exportCmd.Flags().IntVar(&exportLimit, "limit", 0, "Maximum number of traces to export (0 = all)")
	exportCmd.Flags().StringVar(&exportSince, "since", "", "Only export traces after this date (YYYY-MM-DD)")
	Cmd.AddCommand(exportCmd)
}
