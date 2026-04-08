package cmds

import (
	"context"
	"fmt"
	"os"
	"sort"
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

var tracesStatsLimit int
var tracesStatsCommandType string
var tracesStatsCI string
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

var TracesCmd = &cobra.Command{
	Use:   "traces",
	Short: "View and manage build execution traces.",
	Long:  `View, analyze, and export build execution traces for performance analysis and dashboard integration.`,
}

func normalizeTraceCommand(command string) (string, error) {
	switch command {
	case "":
		return "", nil
	case "build", "test", "run":
		return command, nil
	default:
		return "", fmt.Errorf("invalid command %q (use build, test, or run)", command)
	}
}

func normalizeTraceStatsCommandType(commandType string) (string, error) {
	switch commandType {
	case "", "all":
		return "", nil
	case "build", "test":
		return commandType, nil
	default:
		return "", fmt.Errorf("invalid command type %q (use build, test, or all)", commandType)
	}
}

func normalizeTraceStatsCI(ciValue string) (*bool, error) {
	switch ciValue {
	case "", "all":
		return nil, nil
	case "true":
		isCI := true
		return &isCI, nil
	case "false":
		isCI := false
		return &isCI, nil
	default:
		return nil, fmt.Errorf("invalid ci filter %q (use true, false, or all)", ciValue)
	}
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

		command, err := normalizeTraceCommand(tracesListCommand)
		if err != nil {
			logger.Fatalf("%v", err)
		}

		opts := tracing.ListOptions{
			Limit:        tracesListLimit,
			Command:      command,
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

		trace, err := store.FindAndLoad(ctx, args[0])
		if err != nil {
			logger.Fatalf("failed to load trace: %v", err)
		}

		// Sort spans by the requested dimension
		sortSpans(trace.Spans, tracesShowSortBy)

		printBuildSummary(&trace.Build)
		printSpanTable(trace.Spans)
	},
}

var tracesStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate statistics across recent traces.",
	Example: `  grog traces stats
  grog traces stats --command-type build
  grog traces stats --ci true
  grog traces stats --detailed --command-type test`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()

		command, err := normalizeTraceStatsCommandType(tracesStatsCommandType)
		if err != nil {
			logger.Fatalf("%v", err)
		}
		isCI, err := normalizeTraceStatsCI(tracesStatsCI)
		if err != nil {
			logger.Fatalf("%v", err)
		}

		statsOptions := tracing.StatsOptions{
			Limit:   tracesStatsLimit,
			Command: command,
			IsCI:    isCI,
		}

		stats, err := store.Stats(ctx, statsOptions)
		if err != nil {
			logger.Fatalf("failed to compute stats: %v", err)
		}
		if stats.TraceCount == 0 {
			logger.Info("No traces found.")
			return
		}

		printStatsSummary(stats)

		if tracesStatsDetailed {
			report, err := store.Bottlenecks(ctx, statsOptions)
			if err != nil {
				logger.Fatalf("failed to compute bottleneck analysis: %v", err)
			}
			printBottleneckReport(report)
		}
	},
}

var tracesPullCmd = &cobra.Command{
	Use:     "pull",
	Short:   "Download remote traces to local cache for querying.",
	Example: `  grog traces pull`,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getTraceStore(ctx, logger)
		defer store.Close()

		var onProgress tracing.PullProgress
		var teardown func()

		if console.UseTea() {
			teaCtx, program, sendMsg := console.StartTaskUI(ctx)
			ctx = teaCtx

			startedAt := time.Now().Unix()

			onProgress = func(current, total int) {
				sendMsg(console.TaskStateMsg{
					State: console.TaskStateMap{
						0: console.TaskState{
							Status:       fmt.Sprintf("Pulling traces (%d/%d)", current, total),
							StartedAtSec: startedAt,
							Progress: &console.Progress{
								StartedAtSec: startedAt,
								Current:      int64(current),
								Total:        int64(total),
								Unit:         console.ProgressUnitCount,
							},
						},
					},
				})
			}
			teardown = func() {
				program.Quit()
				_ = program.ReleaseTerminal()
			}
		}

		pulled, err := store.Pull(ctx, onProgress)

		if teardown != nil {
			teardown()
		}

		if err != nil {
			logger.Fatalf("pull failed: %v", err)
		}
		logger.Infof("Pulled %d remote trace files.", pulled)
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
	headerStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	sectionStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginTop(1)
	hintStyle       = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("241"))
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

var (
	statsTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	statsLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Width(16)
	statsValueStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	statsGoodStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))  // green
	statsWarnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")) // orange
	statsBadStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")) // red
)

func printStatsSummary(stats *tracing.TraceStats) {
	duration := formatDuration(time.Duration(stats.AvgDuration) * time.Millisecond)
	hitRate := fmt.Sprintf("%.1f%%", stats.CacheHitRate)
	failures := fmt.Sprintf("%d", stats.TotalFails)

	if styled() {
		title := statsTitleStyle.Render(fmt.Sprintf("Stats over last %d traces:", stats.TraceCount))

		// Color-code cache hit rate
		var hitRateStyled string
		if stats.CacheHitRate >= 70 {
			hitRateStyled = statsGoodStyle.Render(hitRate)
		} else if stats.CacheHitRate >= 40 {
			hitRateStyled = statsWarnStyle.Render(hitRate)
		} else {
			hitRateStyled = statsBadStyle.Render(hitRate)
		}

		// Color-code failures
		var failuresStyled string
		if stats.TotalFails == 0 {
			failuresStyled = statsGoodStyle.Render(failures)
		} else {
			failuresStyled = statsBadStyle.Render(failures)
		}

		fmt.Println(title)
		fmt.Printf("  %s %s\n", statsLabelStyle.Render("Avg duration:"), statsValueStyle.Render(duration))
		fmt.Printf("  %s %s\n", statsLabelStyle.Render("Cache hit rate:"), hitRateStyled)
		fmt.Printf("  %s %s\n", statsLabelStyle.Render("Total failures:"), failuresStyled)
	} else {
		fmt.Printf("Stats over last %d traces:\n", stats.TraceCount)
		fmt.Printf("  Avg duration:   %s\n", duration)
		fmt.Printf("  Cache hit rate: %s\n", hitRate)
		fmt.Printf("  Total failures: %s\n", failures)
	}
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

	tracesStatsCmd.Flags().IntVar(&tracesStatsLimit, "limit", 20, "Number of recent traces to aggregate")
	tracesStatsCmd.Flags().StringVar(&tracesStatsCommandType, "command-type", "all", "Filter by build command type (build, test, all)")
	tracesStatsCmd.Flags().StringVar(&tracesStatsCI, "ci", "all", "Filter by CI origin (true, false, all)")
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
	TracesCmd.AddCommand(tracesPullCmd)
	rootCmd.AddCommand(TracesCmd)
}
