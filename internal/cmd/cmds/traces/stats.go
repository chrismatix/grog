package traces

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"grog/internal/console"
	"grog/internal/tracing"
)

var (
	statsLimit       int
	statsCommandType string
	statsCI          string
	statsDetailed    bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate statistics across recent traces.",
	Example: `  grog traces stats
  grog traces stats --command-type build
  grog traces stats --ci true
  grog traces stats --detailed --command-type test`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getStore(ctx, logger)
		defer store.Close()

		command, err := normalizeStatsCommandType(statsCommandType)
		if err != nil {
			logger.Fatalf("%v", err)
		}
		isCI, err := normalizeStatsCI(statsCI)
		if err != nil {
			logger.Fatalf("%v", err)
		}

		statsOptions := tracing.StatsOptions{
			Limit:   statsLimit,
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

		if statsDetailed {
			report, err := store.Bottlenecks(ctx, statsOptions)
			if err != nil {
				logger.Fatalf("failed to compute bottleneck analysis: %v", err)
			}
			printBottleneckReport(report)
		}
	},
}

func registerStatsCmd() {
	statsCmd.Flags().IntVar(&statsLimit, "limit", 20, "Number of recent traces to aggregate")
	statsCmd.Flags().StringVar(&statsCommandType, "command-type", "all", "Filter by build command type (build, test, all)")
	statsCmd.Flags().StringVar(&statsCI, "ci", "all", "Filter by CI origin (true, false, all)")
	statsCmd.Flags().BoolVar(&statsDetailed, "detailed", false, "Load full traces for per-target analysis")
	Cmd.AddCommand(statsCmd)
}

func printStatsSummary(stats *tracing.TraceStats) {
	duration := formatDuration(time.Duration(stats.AvgDuration) * time.Millisecond)
	hitRate := fmt.Sprintf("%.1f%%", stats.CacheHitRate)
	failures := fmt.Sprintf("%d", stats.TotalFails)

	if styled() {
		title := statsTitleStyle.Render(fmt.Sprintf("Stats over last %d traces:", stats.TraceCount))

		var hitRateStyled string
		if stats.CacheHitRate >= 70 {
			hitRateStyled = statsGoodStyle.Render(hitRate)
		} else if stats.CacheHitRate >= 40 {
			hitRateStyled = statsWarnStyle.Render(hitRate)
		} else {
			hitRateStyled = statsBadStyle.Render(hitRate)
		}

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
