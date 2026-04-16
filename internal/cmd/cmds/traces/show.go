package traces

import (
	"fmt"
	"os"
	"sort"
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
	showSortBy string
	showTop    int
)

var showCmd = &cobra.Command{
	Use:   "show <trace-id>",
	Short: "Show details of a specific trace.",
	Example: `  grog traces show a1b2c3d4
  grog traces show a1b2c3d4 --sort-by command --top 10`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getStore(ctx, logger)
		defer store.Close()

		trace, err := store.FindAndLoad(ctx, args[0])
		if err != nil {
			logger.Fatalf("failed to load trace: %v", err)
		}

		sortSpans(trace.Spans, showSortBy)

		printBuildSummary(&trace.Build)
		printSpanTable(trace.Spans)
	},
}

func registerShowCmd() {
	showCmd.Flags().StringVar(&showSortBy, "sort-by", "total", "Sort targets by: total, command, queue, hash")
	showCmd.Flags().IntVar(&showTop, "top", 0, "Show only the N slowest targets (0 = all)")
	Cmd.AddCommand(showCmd)
}

func sortSpans(spans []tracing.SpanRow, sortBy string) {
	switch sortBy {
	case "command":
		sort.Slice(spans, func(i, j int) bool {
			return spans[i].CommandDurationMillis > spans[j].CommandDurationMillis
		})
	case "queue":
		sort.Slice(spans, func(i, j int) bool {
			return spans[i].QueueWaitMillis > spans[j].QueueWaitMillis
		})
	case "hash":
		sort.Slice(spans, func(i, j int) bool {
			return spans[i].HashDurationMillis > spans[j].HashDurationMillis
		})
	default: // "total"
		sort.Slice(spans, func(i, j int) bool {
			return spans[i].TotalDurationMillis > spans[j].TotalDurationMillis
		})
	}
}

func printBuildSummary(b *tracing.BuildRow) {
	t := time.UnixMilli(b.StartTimeUnixMillis)
	duration := formatDuration(time.Duration(b.TotalDurationMillis) * time.Millisecond)
	failures := fmt.Sprintf("%d", b.FailureCount)

	if styled() {
		label := statsLabelStyle.Copy().Width(12)
		val := statsValueStyle

		if b.FailureCount > 0 {
			failures = statsBadStyle.Render(failures)
		} else {
			failures = statsGoodStyle.Render(failures)
		}

		fmt.Printf("%s %s\n", label.Render("Trace:"), renderDim(b.TraceID))
		fmt.Printf("%s %s\n", label.Render("Date:"), val.Render(t.Format("2006-01-02 15:04:05")))
		fmt.Printf("%s %s\n", label.Render("Command:"), val.Render(b.Command))
		fmt.Printf("%s %s\n", label.Render("Version:"), renderDim(b.GrogVersion))
		fmt.Printf("%s %s\n", label.Render("Platform:"), renderDim(b.Platform))
		fmt.Printf("%s %s\n", label.Render("Commit:"), renderDim(b.GitCommit))
		fmt.Printf("%s %s\n", label.Render("Branch:"), val.Render(b.GitBranch))
		fmt.Printf("%s %s\n", label.Render("Duration:"), val.Render(duration))
		fmt.Printf("%s %s (%s cache hits, %s failures)\n",
			label.Render("Targets:"),
			val.Render(fmt.Sprintf("%d", b.TotalTargets)),
			statsGoodStyle.Render(fmt.Sprintf("%d", b.CacheHitCount)),
			failures)

		if b.CriticalPathExecMillis > 0 || b.CriticalPathCacheMillis > 0 {
			fmt.Printf("%s exec %s, cache %s\n",
				label.Render("Critical:"),
				val.Render(formatDuration(time.Duration(b.CriticalPathExecMillis)*time.Millisecond)),
				val.Render(formatDuration(time.Duration(b.CriticalPathCacheMillis)*time.Millisecond)))
		}

		if b.RequestedPatterns != "" {
			fmt.Printf("%s %s\n", label.Render("Patterns:"), renderDim(strings.ReplaceAll(b.RequestedPatterns, ",", ", ")))
		}
	} else {
		fmt.Printf("Trace:    %s\n", b.TraceID)
		fmt.Printf("Date:     %s\n", t.Format("2006-01-02 15:04:05"))
		fmt.Printf("Command:  %s\n", b.Command)
		fmt.Printf("Version:  %s\n", b.GrogVersion)
		fmt.Printf("Platform: %s\n", b.Platform)
		fmt.Printf("Commit:   %s\n", b.GitCommit)
		fmt.Printf("Branch:   %s\n", b.GitBranch)
		fmt.Printf("Duration: %s\n", duration)
		fmt.Printf("Targets:  %d (%d cache hits, %s failures)\n",
			b.TotalTargets, b.CacheHitCount, failures)

		if b.CriticalPathExecMillis > 0 || b.CriticalPathCacheMillis > 0 {
			fmt.Printf("Critical: exec %s, cache %s\n",
				formatDuration(time.Duration(b.CriticalPathExecMillis)*time.Millisecond),
				formatDuration(time.Duration(b.CriticalPathCacheMillis)*time.Millisecond))
		}

		if b.RequestedPatterns != "" {
			fmt.Printf("Patterns: %s\n", strings.ReplaceAll(b.RequestedPatterns, ",", ", "))
		}
	}
	fmt.Println()
}

func printSpanTable(spans []tracing.SpanRow) {
	if len(spans) == 0 {
		return
	}

	displaySpans := spans
	if showTop > 0 && len(displaySpans) > showTop {
		displaySpans = displaySpans[:showTop]
	}

	headers := []string{"TARGET", "STATUS", "CACHE", "TOTAL", "CMD", "HASH", "I/O", "QUEUE"}

	var rows [][]string
	for _, s := range displaySpans {
		status := "ok"
		if s.Status == "FAILURE" {
			status = "FAIL"
		} else if s.Status == "CANCELLED" {
			status = "skip"
		}

		cache := "miss"
		if s.CacheResult == "CACHE_HIT" {
			cache = "hit"
		} else if s.CacheResult == "CACHE_SKIP" {
			cache = "skip"
		}

		ioMillis := s.OutputWriteMillis + s.OutputLoadMillis + s.CacheWriteMillis + s.DepLoadMillis

		row := []string{
			s.Label, status, cache,
			formatMillis(s.TotalDurationMillis),
			formatMillis(s.CommandDurationMillis),
			formatMillis(s.HashDurationMillis),
			formatMillis(ioMillis),
			formatMillis(s.QueueWaitMillis),
		}

		if styled() {
			row[0] = renderLabel(s.Label)
			if status == "FAIL" {
				row[1] = impactHighStyle.Render(status)
			}
			if cache == "hit" {
				row[2] = dimStyle.Render(cache)
			}
		}

		rows = append(rows, row)
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
}
