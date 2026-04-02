package cmds

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/tracing"
)

var tracesListLimit int
var tracesListSince string
var tracesListCommand string
var tracesListFailuresOnly bool

var tracesShowSortBy string
var tracesShowTop int

var tracesStatsDetailed bool

var tracesExportFormat string
var tracesExportOutput string
var tracesExportLimit int
var tracesExportSince string

var tracesPruneOlderThan string

func getTraceStore(ctx context.Context, logger *console.Logger) *tracing.TraceStore {
	cacheConfig := config.Global.Cache
	if config.Global.Traces.Backend != "" {
		cacheConfig = config.CacheConfig{
			Backend: config.Global.Traces.Backend,
			GCS:     config.Global.Traces.GCS,
			S3:      config.Global.Traces.S3,
		}
	}

	cache, err := backends.GetCacheBackend(ctx, cacheConfig)
	if err != nil {
		logger.Fatalf("could not instantiate cache backend for traces: %v", err)
	}

	resolver := tracing.NewPathResolver()
	store, err := tracing.NewTraceStore(cache, resolver)
	if err != nil {
		logger.Fatalf("could not initialize trace store: %v", err)
	}
	return store
}

// autoSync downloads remote traces to local cache if auto_sync is enabled.
func autoSync(ctx context.Context, store *tracing.TraceStore, logger *console.Logger) {
	if !config.Global.Traces.AutoSync {
		return
	}
	synced, err := store.Sync(ctx)
	if err != nil {
		logger.Debugf("auto-sync: %v", err)
		return
	}
	if synced > 0 {
		logger.Infof("Synced %d remote trace files.", synced)
	}
}

var TracesCmd = &cobra.Command{
	Use:   "traces",
	Short: "View and manage build execution traces.",
	Long:  `View, analyze, and export build execution traces for performance analysis and dashboard integration.`,
}

var tracesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent build traces.",
	Example: `  grog traces list
  grog traces list --limit 50
  grog traces list --since 2026-03-01 --command build
  grog traces list --failures-only`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()
		autoSync(ctx, store, logger)

		opts := tracing.ListOptions{
			Limit:        tracesListLimit,
			Command:      tracesListCommand,
			FailuresOnly: tracesListFailuresOnly,
		}
		if tracesListSince != "" {
			sinceTime, err := time.Parse("2006-01-02", tracesListSince)
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

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TRACE ID\tDATE\tCMD\tTARGETS\tHITS\tFAILS\tDURATION\tCOMMIT")
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
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
				shortID,
				t.Format("2006-01-02"),
				e.Command,
				e.TotalTargets,
				e.CacheHitCount,
				e.FailureCount,
				formatDuration(time.Duration(e.TotalDurationMillis)*time.Millisecond),
				commit,
			)
		}
		w.Flush()
	},
}

var tracesShowCmd = &cobra.Command{
	Use:   "show <trace-id>",
	Short: "Show details of a specific trace.",
	Example: `  grog traces show a1b2c3d4
  grog traces show a1b2c3d4 --sort-by command --top 10`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()
		autoSync(ctx, store, logger)

		trace, err := store.FindAndLoad(ctx, args[0])
		if err != nil {
			logger.Fatalf("failed to load trace: %v", err)
		}

		printBuildSummary(&trace.Build)
		printSpanTable(trace.Spans)
	},
}

var tracesStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate statistics across recent traces.",
	Example: `  grog traces stats
  grog traces stats --detailed`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()
		autoSync(ctx, store, logger)

		limit := tracesListLimit
		if limit <= 0 {
			limit = 20
		}

		stats, err := store.Stats(ctx, limit)
		if err != nil {
			logger.Fatalf("failed to compute stats: %v", err)
		}
		if stats.TraceCount == 0 {
			logger.Info("No traces found.")
			return
		}

		fmt.Printf("Stats over last %d traces:\n", stats.TraceCount)
		fmt.Printf("  Avg duration:   %s\n", formatDuration(time.Duration(stats.AvgDuration)*time.Millisecond))
		fmt.Printf("  Cache hit rate: %.1f%%\n", stats.CacheHitRate)
		fmt.Printf("  Total failures: %d\n", stats.TotalFails)

		if tracesStatsDetailed {
			report, err := store.Bottlenecks(ctx, limit)
			if err != nil {
				logger.Fatalf("failed to compute bottleneck analysis: %v", err)
			}
			printBottleneckReport(report)
		}
	},
}

var tracesSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Download remote traces to local cache for querying.",
	Example: `  grog traces sync`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()

		synced, err := store.Sync(ctx)
		if err != nil {
			logger.Fatalf("sync failed: %v", err)
		}
		logger.Infof("Synced %d remote trace files.", synced)
	},
}

var tracesExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export traces for dashboard integration.",
	Example: `  grog traces export --format=jsonl
  grog traces export --format=otel --output traces.json`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()
		autoSync(ctx, store, logger)

		opts := tracing.ListOptions{Limit: tracesExportLimit}
		if tracesExportSince != "" {
			sinceTime, err := time.Parse("2006-01-02", tracesExportSince)
			if err != nil {
				logger.Fatalf("invalid --since date: %v", err)
			}
			opts.Since = &sinceTime
		}

		entries, err := store.List(ctx, opts)
		if err != nil {
			logger.Fatalf("failed to list traces: %v", err)
		}

		var traces []*tracing.BuildTrace
		for _, entry := range entries {
			trace, loadErr := store.FindAndLoad(ctx, entry.TraceID)
			if loadErr != nil {
				logger.Warnf("skipping trace %s: %v", entry.TraceID, loadErr)
				continue
			}
			traces = append(traces, trace)
		}

		if len(traces) == 0 {
			logger.Info("No traces to export.")
			return
		}

		w := os.Stdout
		if tracesExportOutput != "" {
			f, openErr := os.Create(tracesExportOutput)
			if openErr != nil {
				logger.Fatalf("failed to create output file: %v", openErr)
			}
			defer f.Close()
			w = f
		}

		switch tracesExportFormat {
		case "jsonl":
			if err := tracing.ExportJSONL(traces, w); err != nil {
				logger.Fatalf("export failed: %v", err)
			}
		case "otel":
			if err := tracing.ExportOTLP(traces, w); err != nil {
				logger.Fatalf("export failed: %v", err)
			}
		default:
			logger.Fatalf("unknown format %q: use jsonl or otel", tracesExportFormat)
		}
	},
}

var tracesPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Delete traces older than a specified duration.",
	Example: `  grog traces prune --older-than 30d
  grog traces prune --older-than 7d`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()

		duration, err := parseDuration(tracesPruneOlderThan)
		if err != nil {
			logger.Fatalf("invalid --older-than value: %v", err)
		}

		cutoff := time.Now().Add(-duration)
		pruned, pruneErr := store.Prune(ctx, cutoff)
		if pruneErr != nil {
			logger.Fatalf("prune failed: %v", pruneErr)
		}

		logger.Infof("Pruned %d traces older than %s.", pruned, tracesPruneOlderThan)
	},
}

// helpers

func printBuildSummary(b *tracing.BuildRow) {
	t := time.UnixMilli(b.StartTimeUnixMillis)
	fmt.Printf("Trace:    %s\n", b.TraceID)
	fmt.Printf("Date:     %s\n", t.Format("2006-01-02 15:04:05"))
	fmt.Printf("Command:  %s\n", b.Command)
	fmt.Printf("Version:  %s\n", b.GrogVersion)
	fmt.Printf("Platform: %s\n", b.Platform)
	fmt.Printf("Commit:   %s\n", b.GitCommit)
	fmt.Printf("Branch:   %s\n", b.GitBranch)
	fmt.Printf("Duration: %s\n", formatDuration(time.Duration(b.TotalDurationMillis)*time.Millisecond))
	fmt.Printf("Targets:  %d (%d cache hits, %d failures)\n",
		b.TotalTargets, b.CacheHitCount, b.FailureCount)

	if b.CriticalPathExecMillis > 0 || b.CriticalPathCacheMillis > 0 {
		fmt.Printf("Critical: exec %s, cache %s\n",
			formatDuration(time.Duration(b.CriticalPathExecMillis)*time.Millisecond),
			formatDuration(time.Duration(b.CriticalPathCacheMillis)*time.Millisecond))
	}

	if b.RequestedPatterns != "" {
		fmt.Printf("Patterns: %s\n", strings.ReplaceAll(b.RequestedPatterns, ",", ", "))
	}
	fmt.Println()
}

func printSpanTable(spans []tracing.SpanRow) {
	if len(spans) == 0 {
		return
	}

	displaySpans := spans
	if tracesShowTop > 0 && len(displaySpans) > tracesShowTop {
		displaySpans = displaySpans[:tracesShowTop]
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

// styled returns true if lipgloss rendering should be used.
func styled() bool {
	return console.UseTea()
}

// lipgloss styles for bottleneck report
var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	sectionStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginTop(1)
	hintStyle      = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("241"))
	impactHighStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	impactMedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange
	impactLowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("226")) // yellow
	labelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	dimStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func renderImpact(impact float64) string {
	bar := fmt.Sprintf("%.0fms", impact)
	if !styled() {
		return bar
	}
	if impact > 10000 {
		return impactHighStyle.Render(bar)
	} else if impact > 3000 {
		return impactMedStyle.Render(bar)
	}
	return impactLowStyle.Render(bar)
}

func renderSection(title string) string {
	if styled() {
		return sectionStyle.Render(title)
	}
	return "\n" + title
}

func renderHint(text string) string {
	if styled() {
		return hintStyle.Render("  " + text)
	}
	return "  " + text
}

func renderLabel(label string) string {
	if styled() {
		return labelStyle.Render(label)
	}
	return label
}

func renderDim(text string) string {
	if styled() {
		return dimStyle.Render(text)
	}
	return text
}

func printBottleneckReport(r *tracing.BottleneckReport) {
	if len(r.SlowestTargets) > 0 {
		fmt.Println(renderSection("Highest impact targets (avg duration x frequency):"))
		if styled() {
			printBottleneckTable(r.SlowestTargets, func(t tracing.TargetBottleneck) []string {
				return []string{
					renderImpact(t.Impact),
					renderLabel(t.Label),
					formatMillis(int64(t.AvgCmd)),
					fmt.Sprintf("%.0f%%", t.Frequency*100),
					fmt.Sprintf("%d", t.Count),
				}
			}, []string{"IMPACT", "TARGET", "AVG CMD", "FREQ", "N"})
		} else {
			for _, t := range r.SlowestTargets {
				fmt.Printf("  %s  %s  (avg: %s, freq: %.0f%%, n=%d)\n",
					renderImpact(t.Impact), t.Label, formatMillis(int64(t.AvgCmd)),
					t.Frequency*100, t.Count)
			}
		}
	}

	if len(r.QueueSaturated) > 0 {
		fmt.Println(renderSection("Worker pool saturation (avg queue wait > 500ms):"))
		if styled() {
			printBottleneckTable(r.QueueSaturated, func(t tracing.TargetBottleneck) []string {
				return []string{
					formatMillis(int64(t.AvgQueue)),
					renderLabel(t.Label),
					fmt.Sprintf("%d", t.Count),
				}
			}, []string{"AVG QUEUE", "TARGET", "N"})
		} else {
			for _, t := range r.QueueSaturated {
				fmt.Printf("  %s  %s (n=%d)\n", formatMillis(int64(t.AvgQueue)), t.Label, t.Count)
			}
		}
		fmt.Println(renderHint("consider increasing num_workers"))
	}

	if len(r.IOBottlenecks) > 0 {
		fmt.Println(renderSection("I/O bottlenecks (avg I/O time > 1s):"))
		if styled() {
			printBottleneckTable(r.IOBottlenecks, func(t tracing.TargetBottleneck) []string {
				return []string{
					formatMillis(int64(t.AvgIO)),
					renderLabel(t.Label),
					formatMillis(int64(t.AvgOutputWrite)),
					formatMillis(int64(t.AvgOutputLoad)),
					formatMillis(int64(t.AvgCacheWrite)),
					fmt.Sprintf("%d", t.Count),
				}
			}, []string{"AVG I/O", "TARGET", "WRITE", "LOAD", "CACHE", "N"})
		} else {
			for _, t := range r.IOBottlenecks {
				fmt.Printf("  %s  %s (write: %s, load: %s, cache: %s, n=%d)\n",
					formatMillis(int64(t.AvgIO)), t.Label,
					formatMillis(int64(t.AvgOutputWrite)),
					formatMillis(int64(t.AvgOutputLoad)),
					formatMillis(int64(t.AvgCacheWrite)),
					t.Count)
			}
		}
	}

	if len(r.SlowHashing) > 0 {
		fmt.Println(renderSection("Slow hashing (avg hash time > 200ms):"))
		if styled() {
			printBottleneckTable(r.SlowHashing, func(t tracing.TargetBottleneck) []string {
				return []string{
					formatMillis(int64(t.AvgHash)),
					renderLabel(t.Label),
					fmt.Sprintf("%d", t.Count),
				}
			}, []string{"AVG HASH", "TARGET", "N"})
		} else {
			for _, t := range r.SlowHashing {
				fmt.Printf("  %s  %s (n=%d)\n", formatMillis(int64(t.AvgHash)), t.Label, t.Count)
			}
		}
		fmt.Println(renderHint("reduce input glob scope or split into smaller targets"))
	}

	if len(r.FrequentMisses) > 0 {
		fmt.Println(renderSection(fmt.Sprintf("Frequent cache misses (>%.0f%% miss rate, overall avg: %.0f%%):",
			r.OverallCacheMissRate+30, r.OverallCacheMissRate)))
		if styled() {
			printBottleneckTable(r.FrequentMisses, func(t tracing.TargetBottleneck) []string {
				return []string{
					fmt.Sprintf("%.0f%%", t.MissRate),
					renderLabel(t.Label),
					fmt.Sprintf("%d", t.Count),
				}
			}, []string{"MISS RATE", "TARGET", "N"})
		} else {
			for _, t := range r.FrequentMisses {
				fmt.Printf("  %.0f%%  %s (n=%d)\n", t.MissRate, t.Label, t.Count)
			}
		}
	}

	if len(r.FlakyTargets) > 0 {
		fmt.Println(renderSection("Frequently failing targets:"))
		if styled() {
			printBottleneckTable(r.FlakyTargets, func(t tracing.TargetBottleneck) []string {
				return []string{
					fmt.Sprintf("%d/%d", t.Failures, t.Count),
					renderLabel(t.Label),
				}
			}, []string{"FAILS", "TARGET"})
		} else {
			for _, t := range r.FlakyTargets {
				fmt.Printf("  %d/%d  %s\n", t.Failures, t.Count, t.Label)
			}
		}
	}
}

func printBottleneckTable(items []tracing.TargetBottleneck, rowFn func(tracing.TargetBottleneck) []string, headers []string) {
	var rows [][]string
	for _, item := range items {
		rows = append(rows, rowFn(item))
	}

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
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("duration too short: %q", s)
	}

	unit := s[len(s)-1]
	valueStr := s[:len(s)-1]
	var value int
	if _, err := fmt.Sscanf(valueStr, "%d", &value); err != nil {
		return 0, fmt.Errorf("invalid duration value: %q", s)
	}

	switch unit {
	case 'd':
		return time.Duration(value) * 24 * time.Hour, nil
	case 'h':
		return time.Duration(value) * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %q in %q (use d for days, h for hours)", string(unit), s)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func formatMillis(ms int64) string {
	return formatDuration(time.Duration(ms) * time.Millisecond)
}

func AddTracesCmd(rootCmd *cobra.Command) {
	tracesListCmd.Flags().IntVar(&tracesListLimit, "limit", 20, "Maximum number of traces to display")
	tracesListCmd.Flags().StringVar(&tracesListSince, "since", "", "Only show traces after this date (YYYY-MM-DD)")
	tracesListCmd.Flags().StringVar(&tracesListCommand, "command", "", "Filter by command type (build, test, run)")
	tracesListCmd.Flags().BoolVar(&tracesListFailuresOnly, "failures-only", false, "Only show traces with failures")

	tracesShowCmd.Flags().StringVar(&tracesShowSortBy, "sort-by", "total", "Sort targets by: total, command, queue, hash")
	tracesShowCmd.Flags().IntVar(&tracesShowTop, "top", 0, "Show only the N slowest targets (0 = all)")

	tracesStatsCmd.Flags().IntVar(&tracesListLimit, "limit", 20, "Number of recent traces to aggregate")
	tracesStatsCmd.Flags().BoolVar(&tracesStatsDetailed, "detailed", false, "Load full traces for per-target analysis")

	tracesExportCmd.Flags().StringVar(&tracesExportFormat, "format", "jsonl", "Export format: jsonl or otel")
	tracesExportCmd.Flags().StringVar(&tracesExportOutput, "output", "", "Output file (default: stdout)")
	tracesExportCmd.Flags().IntVar(&tracesExportLimit, "limit", 0, "Maximum number of traces to export (0 = all)")
	tracesExportCmd.Flags().StringVar(&tracesExportSince, "since", "", "Only export traces after this date (YYYY-MM-DD)")

	tracesPruneCmd.Flags().StringVar(&tracesPruneOlderThan, "older-than", "30d", "Delete traces older than this duration (e.g. 30d, 72h)")

	TracesCmd.AddCommand(tracesListCmd)
	TracesCmd.AddCommand(tracesShowCmd)
	TracesCmd.AddCommand(tracesStatsCmd)
	TracesCmd.AddCommand(tracesExportCmd)
	TracesCmd.AddCommand(tracesPruneCmd)
	TracesCmd.AddCommand(tracesSyncCmd)
	rootCmd.AddCommand(TracesCmd)
}
