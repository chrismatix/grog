package traces

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"grog/internal/console"
	"grog/internal/tracing"
)

var (
	listLimit        int
	listSince        string
	listCommand      string
	listFailuresOnly bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent build traces.",
	Example: `  grog traces list
  grog traces list --limit 50
  grog traces list --since 2026-03-01 --command build
  grog traces list --failures-only`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getStore(ctx, logger)
		defer store.Close()

		command, err := normalizeCommand(listCommand)
		if err != nil {
			logger.Fatalf("%v", err)
		}

		opts := tracing.ListOptions{
			Limit:        listLimit,
			Command:      command,
			FailuresOnly: listFailuresOnly,
		}
		if listSince != "" {
			sinceTime, err := time.Parse("2006-01-02", listSince)
			if err != nil {
				logger.Fatalf("invalid --since date: %v (use YYYY-MM-DD format)", err)
			}
			opts.Since = &sinceTime
		}

		entries, err := store.List(ctx, opts)
		if err != nil {
			logger.Fatalf("failed to list traces: %v", err)
		}
		if len(entries) == 0 {
			logger.Info("No traces found.")
			return
		}

		headers := []string{"TRACE ID", "DATE", "CMD", "TARGETS", "HITS", "FAILS", "DURATION", "COMMIT"}
		var rows [][]string
		for _, e := range entries {
			t := time.UnixMilli(e.StartTimeUnixMillis)
			shortID := e.TraceID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			commit := e.GitCommit
			if len(commit) > 7 {
				commit = commit[:7]
			}

			fails := fmt.Sprintf("%d", e.FailureCount)
			if styled() && e.FailureCount > 0 {
				fails = statsBadStyle.Render(fails)
			}

			rows = append(rows, []string{
				shortID,
				t.Format("2006-01-02"),
				e.Command,
				fmt.Sprintf("%d", e.TotalTargets),
				fmt.Sprintf("%d", e.CacheHitCount),
				fails,
				formatDuration(time.Duration(e.TotalDurationMillis) * time.Millisecond),
				renderDim(commit),
			})
		}

		if styled() {
			t := table.New().
				Headers(headers...).
				Rows(rows...).
				Border(lipgloss.NormalBorder()).
				BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("238"))).
				StyleFunc(func(row, col int) lipgloss.Style {
					if row == table.HeaderRow {
						return headerStyle
					}
					return lipgloss.NewStyle()
				})
			fmt.Println(t.Render())
		} else {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, strings.Join(headers, "\t"))
			for _, row := range rows {
				fmt.Fprintln(w, strings.Join(row, "\t"))
			}
			w.Flush()
		}
	},
}

func registerListCmd() {
	listCmd.Flags().IntVar(&listLimit, "limit", 20, "Maximum number of traces to display")
	listCmd.Flags().StringVar(&listSince, "since", "", "Only show traces after this date (YYYY-MM-DD)")
	listCmd.Flags().StringVar(&listCommand, "command", "", "Filter by command type (build, test, run)")
	listCmd.Flags().BoolVar(&listFailuresOnly, "failures-only", false, "Only show traces with failures")
	Cmd.AddCommand(listCmd)
}
