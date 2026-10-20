package traces

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/console"
	"github.com/chrismatix/grog/internal/tracing"
)

var (
	rootCmd      = &cobra.Command{Use: "grog"}
	registerOnce sync.Once
)

func TestCommands(t *testing.T) {
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	config.Global.Root = t.TempDir()
	config.Global.Traces.Backend = "local"

	registerOnce.Do(func() { AddCmd(rootCmd) })

	ctx := context.Background()
	store := getStore(ctx, console.GetLogger(ctx))
	startTime := time.Now()
	for index, traceID := range []string{"11111111-aaaa", "22222222-bbbb"} {
		trace := &tracing.BuildTrace{
			Build: tracing.BuildRow{
				TraceID:             traceID,
				Command:             "build",
				StartTimeUnixMillis: startTime.Add(time.Duration(index) * time.Minute).UnixMilli(),
				TotalDurationMillis: 5000,
				TotalTargets:        1,
				FailureCount:        1,
				GitCommit:           "abcdef1234567",
			},
			Spans: []tracing.SpanRow{{
				TraceID:               traceID,
				Label:                 "//pkg:target",
				Package:               "pkg",
				Status:                "FAILURE",
				CacheResult:           "CACHE_MISS",
				TotalDurationMillis:   4000,
				CommandDurationMillis: 3000,
				HashDurationMillis:    500,
				QueueWaitMillis:       800,
				OutputWriteMillis:     1500,
			}},
		}
		if err := store.Write(ctx, trace); err != nil {
			t.Fatalf("failed to write trace: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	exportPath := filepath.Join(t.TempDir(), "traces.jsonl")
	tests := []struct {
		name      string
		arguments []string
		expected  []string
	}{
		{
			name:      "list",
			arguments: []string{"list", "--command=build", "--since=2000-01-01"},
			expected:  []string{"11111111", "22222222", "abcdef1"},
		},
		{
			name:      "list no match",
			arguments: []string{"list", "--command=test", "--since=2000-01-01"},
		},
		{
			name:      "show",
			arguments: []string{"show", "1111", "--sort-by=command", "--top=1"},
			expected:  []string{"11111111-aaaa", "//pkg:target"},
		},
		{
			name:      "stats detailed",
			arguments: []string{"stats", "--command-type=build", "--ci=all", "--detailed"},
			expected:  []string{"Stats over last 2 traces:", "Highest impact targets"},
		},
		{
			name:      "stats no match",
			arguments: []string{"stats", "--command-type=test", "--ci=true", "--detailed=false"},
		},
		{
			name:      "export jsonl to stdout",
			arguments: []string{"export", "--format=jsonl", "--since=2000-01-01"},
			expected:  []string{"11111111-aaaa"},
		},
		{
			name:      "export otel to file",
			arguments: []string{"export", "--format=otel", "--output=" + exportPath},
		},
		{
			name:      "export no match",
			arguments: []string{"export", "--output=", "--since=2999-01-01"},
		},
		{
			name:      "pull",
			arguments: []string{"pull"},
		},
		{
			name:      "prune nothing",
			arguments: []string{"prune", "--older-than=1d"},
		},
		{
			name:      "prune all",
			arguments: []string{"prune", "--older-than=0h"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rootCmd.SetArgs(append([]string{"traces"}, test.arguments...))
			output := captureOutput(t, false, func() {
				if err := rootCmd.Execute(); err != nil {
					t.Fatalf("command failed: %v", err)
				}
			})
			for _, expected := range test.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, got %q", expected, output)
				}
			}
		})
	}

	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("failed to read export: %v", err)
	}
	if !strings.Contains(string(exported), "resourceSpans") {
		t.Fatalf("expected OTLP export, got %q", exported)
	}
}
