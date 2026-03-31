package cmds

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	gen "grog/internal/proto/gen"
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
	// Use traces-specific backend if configured, otherwise fall back to cache backend
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
	return tracing.NewTraceStore(cache)
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

		entries, err := store.List(ctx)
		if err != nil {
			logger.Fatalf("failed to list traces: %v", err)
		}

		// Apply filters
		if tracesListSince != "" {
			sinceTime, parseErr := time.Parse("2006-01-02", tracesListSince)
			if parseErr != nil {
				logger.Fatalf("invalid --since date: %v (use YYYY-MM-DD format)", parseErr)
			}
			filtered := entries[:0]
			for _, e := range entries {
				if time.UnixMilli(e.StartTimeUnixMillis).After(sinceTime) || time.UnixMilli(e.StartTimeUnixMillis).Equal(sinceTime) {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		if tracesListCommand != "" {
			filtered := entries[:0]
			for _, e := range entries {
				if e.Command == tracesListCommand {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		if tracesListFailuresOnly {
			filtered := entries[:0]
			for _, e := range entries {
				if e.FailureCount > 0 {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		if tracesListLimit > 0 && len(entries) > tracesListLimit {
			entries = entries[:tracesListLimit]
		}

		if len(entries) == 0 {
			logger.Info("No traces found.")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TRACE ID\tDATE\tCMD\tTARGETS\tHITS\tFAILS\tDURATION\tCOMMIT")
		for _, e := range entries {
			t := time.UnixMilli(e.StartTimeUnixMillis)
			shortID := e.TraceId
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

		trace, err := store.FindAndLoad(ctx, args[0])
		if err != nil {
			logger.Fatalf("failed to load trace: %v", err)
		}

		printTraceSummary(trace)
		printTraceSpans(trace.Spans)
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

		entries, err := store.List(ctx)
		if err != nil {
			logger.Fatalf("failed to list traces: %v", err)
		}

		if len(entries) == 0 {
			logger.Info("No traces found.")
			return
		}

		limit := tracesListLimit
		if limit <= 0 {
			limit = 20
		}
		if len(entries) > limit {
			entries = entries[:limit]
		}

		printIndexStats(entries)

		if tracesStatsDetailed {
			fmt.Println()
			printDetailedStats(ctx, store, entries, logger)
		}
	},
}

func printTraceSummary(trace *gen.BuildTrace) {
	t := time.UnixMilli(trace.StartTimeUnixMillis)
	fmt.Printf("Trace:    %s\n", trace.TraceId)
	fmt.Printf("Date:     %s\n", t.Format("2006-01-02 15:04:05"))
	fmt.Printf("Command:  %s\n", trace.Command)
	fmt.Printf("Version:  %s\n", trace.GrogVersion)
	fmt.Printf("Platform: %s\n", trace.Platform)
	fmt.Printf("Commit:   %s\n", trace.GitCommit)
	fmt.Printf("Branch:   %s\n", trace.GitBranch)
	fmt.Printf("Duration: %s\n", formatDuration(time.Duration(trace.TotalDurationMillis)*time.Millisecond))
	fmt.Printf("Targets:  %d (%d cache hits, %d failures)\n",
		trace.TotalTargets, trace.CacheHitCount, trace.FailureCount)

	if trace.CriticalPathExecMillis > 0 || trace.CriticalPathCacheMillis > 0 {
		fmt.Printf("Critical: exec %s, cache %s\n",
			formatDuration(time.Duration(trace.CriticalPathExecMillis)*time.Millisecond),
			formatDuration(time.Duration(trace.CriticalPathCacheMillis)*time.Millisecond))
	}

	if len(trace.RequestedPatterns) > 0 {
		fmt.Printf("Patterns: %s\n", strings.Join(trace.RequestedPatterns, ", "))
	}
	fmt.Println()
}

func printTraceSpans(spans []*gen.TargetSpan) {
	if len(spans) == 0 {
		return
	}

	// Sort by specified field
	sortSpans(spans)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TARGET\tSTATUS\tCACHE\tTOTAL\tCMD\tHASH\tI/O\tQUEUE")

	displaySpans := spans
	if tracesShowTop > 0 && len(displaySpans) > tracesShowTop {
		displaySpans = displaySpans[:tracesShowTop]
	}

	for _, s := range displaySpans {
		status := "ok"
		if s.Status == gen.TargetSpan_FAILURE {
			status = "FAIL"
		} else if s.Status == gen.TargetSpan_CANCELLED {
			status = "skip"
		}

		cache := "miss"
		if s.CacheResult == gen.TargetSpan_CACHE_HIT {
			cache = "hit"
		} else if s.CacheResult == gen.TargetSpan_CACHE_SKIP {
			cache = "skip"
		}

		ioMillis := s.OutputWriteMillis + s.OutputLoadMillis + s.CacheWriteMillis + s.DepLoadMillis

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Label,
			status,
			cache,
			formatMillis(s.TotalDurationMillis),
			formatMillis(s.CommandDurationMillis),
			formatMillis(s.HashDurationMillis),
			formatMillis(ioMillis),
			formatMillis(s.QueueWaitMillis),
		)
	}
	w.Flush()
}

func sortSpans(spans []*gen.TargetSpan) {
	sort.Slice(spans, func(i, j int) bool {
		switch tracesShowSortBy {
		case "command":
			return spans[i].CommandDurationMillis > spans[j].CommandDurationMillis
		case "queue":
			return spans[i].QueueWaitMillis > spans[j].QueueWaitMillis
		case "hash":
			return spans[i].HashDurationMillis > spans[j].HashDurationMillis
		default: // "total"
			return spans[i].TotalDurationMillis > spans[j].TotalDurationMillis
		}
	})
}

func printIndexStats(entries []*gen.TraceIndexEntry) {
	var totalDuration int64
	var totalTargets int32
	var totalCacheHits int32
	var totalFailures int32

	for _, e := range entries {
		totalDuration += e.TotalDurationMillis
		totalTargets += e.TotalTargets
		totalCacheHits += e.CacheHitCount
		totalFailures += e.FailureCount
	}

	n := int64(len(entries))
	avgDuration := time.Duration(totalDuration/n) * time.Millisecond

	var cacheHitRate float64
	if totalTargets > 0 {
		cacheHitRate = float64(totalCacheHits) / float64(totalTargets) * 100
	}

	fmt.Printf("Stats over last %d traces:\n", len(entries))
	fmt.Printf("  Avg duration:  %s\n", formatDuration(avgDuration))
	fmt.Printf("  Cache hit rate: %.1f%%\n", cacheHitRate)
	fmt.Printf("  Total failures: %d\n", totalFailures)
}

func printDetailedStats(ctx context.Context, store *tracing.TraceStore, entries []*gen.TraceIndexEntry, logger *console.Logger) {
	type targetStats struct {
		totalCommandMillis int64
		totalQueueMillis   int64
		totalIOMillis      int64
		failures           int
		count              int
	}

	stats := make(map[string]*targetStats)

	for _, entry := range entries {
		date := time.UnixMilli(entry.StartTimeUnixMillis).UTC().Format("2006-01-02")
		trace, err := store.Load(ctx, entry.TraceId, date)
		if err != nil {
			logger.Warnf("failed to load trace %s: %v", entry.TraceId, err)
			continue
		}

		for _, span := range trace.Spans {
			s, ok := stats[span.Label]
			if !ok {
				s = &targetStats{}
				stats[span.Label] = s
			}
			s.totalCommandMillis += span.CommandDurationMillis
			s.totalQueueMillis += span.QueueWaitMillis
			s.totalIOMillis += span.OutputWriteMillis + span.OutputLoadMillis + span.CacheWriteMillis
			s.count++
			if span.Status == gen.TargetSpan_FAILURE {
				s.failures++
			}
		}
	}

	type rankedTarget struct {
		label string
		value int64
		count int
	}

	// Slowest by average command duration
	var byCommand []rankedTarget
	for label, s := range stats {
		if s.count > 0 {
			byCommand = append(byCommand, rankedTarget{label, s.totalCommandMillis / int64(s.count), s.count})
		}
	}
	sort.Slice(byCommand, func(i, j int) bool { return byCommand[i].value > byCommand[j].value })

	fmt.Println("Slowest targets (avg command duration):")
	for i, t := range byCommand {
		if i >= 10 {
			break
		}
		fmt.Printf("  %s  %s (n=%d)\n", formatMillis(t.value), t.label, t.count)
	}

	// Highest queue wait
	var byQueue []rankedTarget
	for label, s := range stats {
		if s.count > 0 {
			byQueue = append(byQueue, rankedTarget{label, s.totalQueueMillis / int64(s.count), s.count})
		}
	}
	sort.Slice(byQueue, func(i, j int) bool { return byQueue[i].value > byQueue[j].value })

	fmt.Println("\nHighest queue wait (avg):")
	for i, t := range byQueue {
		if i >= 5 {
			break
		}
		fmt.Printf("  %s  %s (n=%d)\n", formatMillis(t.value), t.label, t.count)
	}

	// Most frequent failures
	var byFailures []rankedTarget
	for label, s := range stats {
		if s.failures > 0 {
			byFailures = append(byFailures, rankedTarget{label, int64(s.failures), s.count})
		}
	}
	sort.Slice(byFailures, func(i, j int) bool { return byFailures[i].value > byFailures[j].value })

	if len(byFailures) > 0 {
		fmt.Println("\nMost frequently failing targets:")
		for i, t := range byFailures {
			if i >= 5 {
				break
			}
			fmt.Printf("  %d/%d  %s\n", t.value, t.count, t.label)
		}
	}
}

var tracesExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export traces for dashboard integration.",
	Example: `  grog traces export --format=jsonl
  grog traces export --format=otel --output traces.json
  grog traces export --format=jsonl --since 2026-03-01 --limit 100`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)

		entries, err := store.List(ctx)
		if err != nil {
			logger.Fatalf("failed to list traces: %v", err)
		}

		if tracesExportSince != "" {
			sinceTime, parseErr := time.Parse("2006-01-02", tracesExportSince)
			if parseErr != nil {
				logger.Fatalf("invalid --since date: %v", parseErr)
			}
			filtered := entries[:0]
			for _, e := range entries {
				if time.UnixMilli(e.StartTimeUnixMillis).After(sinceTime) || time.UnixMilli(e.StartTimeUnixMillis).Equal(sinceTime) {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		if tracesExportLimit > 0 && len(entries) > tracesExportLimit {
			entries = entries[:tracesExportLimit]
		}

		// Load full traces
		var traces []*gen.BuildTrace
		for _, entry := range entries {
			date := time.UnixMilli(entry.StartTimeUnixMillis).UTC().Format("2006-01-02")
			trace, loadErr := store.Load(ctx, entry.TraceId, date)
			if loadErr != nil {
				logger.Warnf("skipping trace %s: %v", entry.TraceId, loadErr)
				continue
			}
			traces = append(traces, trace)
		}

		if len(traces) == 0 {
			logger.Info("No traces to export.")
			return
		}

		// Determine output writer
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
	// list subcommand
	tracesListCmd.Flags().IntVar(&tracesListLimit, "limit", 20, "Maximum number of traces to display")
	tracesListCmd.Flags().StringVar(&tracesListSince, "since", "", "Only show traces after this date (YYYY-MM-DD)")
	tracesListCmd.Flags().StringVar(&tracesListCommand, "command", "", "Filter by command type (build, test, run)")
	tracesListCmd.Flags().BoolVar(&tracesListFailuresOnly, "failures-only", false, "Only show traces with failures")

	// show subcommand
	tracesShowCmd.Flags().StringVar(&tracesShowSortBy, "sort-by", "total", "Sort targets by: total, command, queue, hash")
	tracesShowCmd.Flags().IntVar(&tracesShowTop, "top", 0, "Show only the N slowest targets (0 = all)")

	// stats subcommand
	tracesStatsCmd.Flags().IntVar(&tracesListLimit, "limit", 20, "Number of recent traces to aggregate")
	tracesStatsCmd.Flags().BoolVar(&tracesStatsDetailed, "detailed", false, "Load full traces for per-target analysis")

	// export subcommand
	tracesExportCmd.Flags().StringVar(&tracesExportFormat, "format", "jsonl", "Export format: jsonl or otel")
	tracesExportCmd.Flags().StringVar(&tracesExportOutput, "output", "", "Output file (default: stdout)")
	tracesExportCmd.Flags().IntVar(&tracesExportLimit, "limit", 0, "Maximum number of traces to export (0 = all)")
	tracesExportCmd.Flags().StringVar(&tracesExportSince, "since", "", "Only export traces after this date (YYYY-MM-DD)")

	// prune subcommand
	tracesPruneCmd.Flags().StringVar(&tracesPruneOlderThan, "older-than", "30d", "Delete traces older than this duration (e.g. 30d, 72h)")

	TracesCmd.AddCommand(tracesListCmd)
	TracesCmd.AddCommand(tracesShowCmd)
	TracesCmd.AddCommand(tracesStatsCmd)
	TracesCmd.AddCommand(tracesExportCmd)
	TracesCmd.AddCommand(tracesPruneCmd)
	rootCmd.AddCommand(TracesCmd)
}
