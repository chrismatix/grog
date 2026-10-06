package tracing

import (
	"context"
	"slices"
	"testing"
	"time"

	"grog/internal/caching/backends"

	"github.com/parquet-go/parquet-go"
)

func newTestWriter(t *testing.T) (*TraceWriter, string) {
	t.Helper()
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	return NewTraceWriter(fs), dir
}

func makeTestTrace(id string, startMillis int64, command string) *BuildTrace {
	return &BuildTrace{
		Build: BuildRow{
			TraceID:             id,
			Workspace:           "test-workspace",
			Command:             command,
			StartTimeUnixMillis: startMillis,
			TotalDurationMillis: 5000,
			TotalTargets:        10,
			SuccessCount:        8,
			FailureCount:        2,
			CacheHitCount:       6,
			GitCommit:           "abc1234",
		},
		Spans: []SpanRow{
			{
				TraceID:               id,
				Label:                 "//pkg:target",
				Package:               "pkg",
				Status:                "SUCCESS",
				CacheResult:           "CACHE_MISS",
				StartTimeUnixMillis:   startMillis + 100,
				EndTimeUnixMillis:     startMillis + 2100,
				TotalDurationMillis:   2000,
				CommandDurationMillis: 1500,
				HashDurationMillis:    50,
				QueueWaitMillis:       200,
			},
		},
	}
}

func makeTestTraceWithCI(id string, startMillis int64, command string, isCI bool) *BuildTrace {
	trace := makeTestTrace(id, startMillis, command)
	trace.Build.IsCI = isCI
	return trace
}

func TestTraceWriter_WriteParquet(t *testing.T) {
	writer, dir := newTestWriter(t)
	ctx := context.Background()

	now := time.Now()
	trace := makeTestTrace("trace-001", now.UnixMilli(), "build")

	err := writer.Write(ctx, trace)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify builds Parquet file can be read back
	date := now.UTC().Format("2006-01-02")
	buildsFile := dir + "/traces/builds/" + date + "/trace-001.parquet"

	builds, err := parquet.ReadFile[BuildRow](buildsFile)
	if err != nil {
		t.Fatalf("Read builds parquet failed: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected 1 build row, got %d", len(builds))
	}
	if builds[0].TraceID != "trace-001" {
		t.Errorf("expected trace ID trace-001, got %s", builds[0].TraceID)
	}
	if builds[0].TotalTargets != 10 {
		t.Errorf("expected 10 targets, got %d", builds[0].TotalTargets)
	}

	// Verify spans Parquet file
	spansFile := dir + "/traces/spans/" + date + "/trace-001.parquet"
	spans, err := parquet.ReadFile[SpanRow](spansFile)
	if err != nil {
		t.Fatalf("Read spans parquet failed: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span row, got %d", len(spans))
	}
	if spans[0].CommandDurationMillis != 1500 {
		t.Errorf("expected 1500ms command duration, got %d", spans[0].CommandDurationMillis)
	}
	if spans[0].Label != "//pkg:target" {
		t.Errorf("expected //pkg:target, got %s", spans[0].Label)
	}
}

func TestTraceStore_ListAndLoad(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	writer := NewTraceWriter(fs)
	ctx := context.Background()

	now := time.Now()
	// Write 3 traces
	for i, id := range []string{"trace-a", "trace-b", "trace-c"} {
		trace := makeTestTrace(id, now.Add(time.Duration(i)*time.Minute).UnixMilli(), "build")
		if err := writer.Write(ctx, trace); err != nil {
			t.Fatalf("Write %s failed: %v", id, err)
		}
	}

	// Create a store with the FS path as resolver
	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore failed: %v", err)
	}
	defer store.Close()

	// List
	entries, err := store.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Should be sorted newest first
	if entries[0].TraceID != "trace-c" {
		t.Errorf("expected newest first (trace-c), got %s", entries[0].TraceID)
	}

	// FindAndLoad
	trace, err := store.FindAndLoad(ctx, "trace-b")
	if err != nil {
		t.Fatalf("FindAndLoad failed: %v", err)
	}
	if trace.Build.TraceID != "trace-b" {
		t.Errorf("expected trace-b, got %s", trace.Build.TraceID)
	}
	if len(trace.Spans) != 1 {
		t.Errorf("expected 1 span, got %d", len(trace.Spans))
	}
}

func TestTraceStore_Stats(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	writer := NewTraceWriter(fs)
	ctx := context.Background()

	now := time.Now()
	for i, id := range []string{"s1", "s2"} {
		trace := makeTestTraceWithCI(id, now.Add(time.Duration(i)*time.Minute).UnixMilli(), "build", false)
		if err := writer.Write(ctx, trace); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore failed: %v", err)
	}
	defer store.Close()

	testCases := []struct {
		name               string
		options            StatsOptions
		expectedTraceCount int
		expectedTotalFails int
	}{
		{
			name: "all commands",
			options: StatsOptions{
				Limit: 10,
			},
			expectedTraceCount: 2,
			expectedTotalFails: 4,
		},
		{
			name: "build only",
			options: StatsOptions{
				Limit:   10,
				Command: "build",
			},
			expectedTraceCount: 2,
			expectedTotalFails: 4,
		},
		{
			name: "test only no matches",
			options: StatsOptions{
				Limit:   10,
				Command: "test",
			},
			expectedTraceCount: 0,
			expectedTotalFails: 0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stats, err := store.Stats(ctx, testCase.options)
			if err != nil {
				t.Fatalf("Stats failed: %v", err)
			}
			if stats.TraceCount != testCase.expectedTraceCount {
				t.Errorf("expected %d traces, got %d", testCase.expectedTraceCount, stats.TraceCount)
			}
			if stats.TotalFails != testCase.expectedTotalFails {
				t.Errorf("expected %d total failures, got %d", testCase.expectedTotalFails, stats.TotalFails)
			}
		})
	}
}

func TestTraceStore_StatsFiltersByCommand(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	writer := NewTraceWriter(fs)
	ctx := context.Background()

	now := time.Now()
	if err := writer.Write(ctx, makeTestTraceWithCI("build-trace", now.UnixMilli(), "build", false)); err != nil {
		t.Fatalf("Write build trace failed: %v", err)
	}
	if err := writer.Write(ctx, makeTestTraceWithCI("test-trace", now.Add(time.Minute).UnixMilli(), "test", true)); err != nil {
		t.Fatalf("Write test trace failed: %v", err)
	}

	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore failed: %v", err)
	}
	defer store.Close()

	testCases := []struct {
		name               string
		options            StatsOptions
		expectedTraceCount int
	}{
		{
			name: "all",
			options: StatsOptions{
				Limit: 10,
			},
			expectedTraceCount: 2,
		},
		{
			name: "build",
			options: StatsOptions{
				Limit:   10,
				Command: "build",
			},
			expectedTraceCount: 1,
		},
		{
			name: "test",
			options: StatsOptions{
				Limit:   10,
				Command: "test",
			},
			expectedTraceCount: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stats, err := store.Stats(ctx, testCase.options)
			if err != nil {
				t.Fatalf("Stats failed: %v", err)
			}
			if stats.TraceCount != testCase.expectedTraceCount {
				t.Errorf("expected %d traces, got %d", testCase.expectedTraceCount, stats.TraceCount)
			}
		})
	}
}

func TestTraceStore_StatsFiltersByCI(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	writer := NewTraceWriter(fs)
	ctx := context.Background()

	now := time.Now()
	if err := writer.Write(ctx, makeTestTraceWithCI("local-trace", now.UnixMilli(), "build", false)); err != nil {
		t.Fatalf("Write local trace failed: %v", err)
	}
	if err := writer.Write(ctx, makeTestTraceWithCI("ci-trace", now.Add(time.Minute).UnixMilli(), "build", true)); err != nil {
		t.Fatalf("Write CI trace failed: %v", err)
	}

	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore failed: %v", err)
	}
	defer store.Close()

	trueValue := true
	falseValue := false
	testCases := []struct {
		name               string
		options            StatsOptions
		expectedTraceCount int
	}{
		{
			name: "all",
			options: StatsOptions{
				Limit: 10,
			},
			expectedTraceCount: 2,
		},
		{
			name: "ci only",
			options: StatsOptions{
				Limit: 10,
				IsCI:  &trueValue,
			},
			expectedTraceCount: 1,
		},
		{
			name: "non ci only",
			options: StatsOptions{
				Limit: 10,
				IsCI:  &falseValue,
			},
			expectedTraceCount: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stats, err := store.Stats(ctx, testCase.options)
			if err != nil {
				t.Fatalf("Stats failed: %v", err)
			}
			if stats.TraceCount != testCase.expectedTraceCount {
				t.Errorf("expected %d traces, got %d", testCase.expectedTraceCount, stats.TraceCount)
			}
		})
	}
}

func TestTraceStore_Prune(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	writer := NewTraceWriter(fs)
	ctx := context.Background()

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	if err := writer.Write(ctx, makeTestTrace("old-trace", old.UnixMilli(), "build")); err != nil {
		t.Fatalf("Write old trace failed: %v", err)
	}
	if err := writer.Write(ctx, makeTestTrace("new-trace", recent.UnixMilli(), "build")); err != nil {
		t.Fatalf("Write new trace failed: %v", err)
	}

	resolver := &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	}
	store, err := NewTraceStore(fs, resolver)
	if err != nil {
		t.Fatalf("NewTraceStore failed: %v", err)
	}
	defer store.Close()

	pruned, err := store.Prune(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	// Verify only new trace remains
	entries, err := store.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after prune, got %d", len(entries))
	}
	if entries[0].TraceID != "new-trace" {
		t.Errorf("expected new-trace, got %s", entries[0].TraceID)
	}
}

func TestTraceStore_Bottlenecks(t *testing.T) {
	dir := t.TempDir()
	fs := backends.NewFileSystemCacheForTest(dir, t.TempDir())
	writer := NewTraceWriter(fs)
	ctx := context.Background()

	span := func(label string, modify func(*SpanRow)) SpanRow {
		row := SpanRow{Label: label, Status: "SUCCESS", CacheResult: "CACHE_HIT", CommandDurationMillis: 100}
		modify(&row)
		return row
	}
	now := time.Now()
	for index, traceID := range []string{"bottleneck-1", "bottleneck-2"} {
		trace := makeTestTrace(traceID, now.Add(time.Duration(index)*time.Minute).UnixMilli(), "build")
		trace.Spans = []SpanRow{
			span("//pkg:always_miss", func(row *SpanRow) { row.CacheResult = "CACHE_MISS"; row.CommandDurationMillis = 5000 }),
			span("//pkg:queued", func(row *SpanRow) { row.QueueWaitMillis = 800 }),
			span("//pkg:io_heavy", func(row *SpanRow) { row.OutputWriteMillis = 1500 }),
			span("//pkg:slow_hash", func(row *SpanRow) { row.HashDurationMillis = 300 }),
			span("//pkg:flaky", func(row *SpanRow) {
				if index == 0 {
					row.Status = "FAILURE"
				}
			}),
		}
		if index == 0 {
			trace.Spans = append(trace.Spans, span("//pkg:only_once", func(row *SpanRow) { row.QueueWaitMillis = 9000 }))
		}
		for spanIndex := range trace.Spans {
			trace.Spans[spanIndex].TraceID = traceID
		}
		if err := writer.Write(ctx, trace); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	store, err := NewTraceStore(fs, &PathResolver{
		buildsBase: dir + "/traces/builds",
		spansBase:  dir + "/traces/spans",
	})
	if err != nil {
		t.Fatalf("NewTraceStore failed: %v", err)
	}
	defer store.Close()

	report, err := store.Bottlenecks(ctx, StatsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Bottlenecks failed: %v", err)
	}

	if count := len(report.SlowestTargets); count != 5 {
		t.Fatalf("expected 5 targets seen in more than one build, got %d", count)
	}

	labels := func(targets []TargetBottleneck) []string {
		var result []string
		for _, target := range targets {
			result = append(result, target.Label)
		}
		return result
	}
	testCases := []struct {
		category string
		actual   []TargetBottleneck
		expected []string
	}{
		{"slowest", report.SlowestTargets[:1], []string{"//pkg:always_miss"}},
		{"queue saturated", report.QueueSaturated, []string{"//pkg:queued"}},
		{"io", report.IOBottlenecks, []string{"//pkg:io_heavy"}},
		{"slow hashing", report.SlowHashing, []string{"//pkg:slow_hash"}},
		{"frequent misses", report.FrequentMisses, []string{"//pkg:always_miss"}},
		{"flaky", report.FlakyTargets, []string{"//pkg:flaky"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.category, func(t *testing.T) {
			if actual := labels(testCase.actual); !slices.Equal(actual, testCase.expected) {
				t.Errorf("expected %v, got %v", testCase.expected, actual)
			}
		})
	}
}

func TestTraceStore_Pull(t *testing.T) {
	ctx := context.Background()
	localDir := t.TempDir()
	local := backends.NewFileSystemCacheForTest(localDir, t.TempDir())
	remote := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())

	now := time.Now()
	if err := NewTraceWriter(remote).Write(ctx, makeTestTrace("remote-only", now.UnixMilli(), "build")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	shared := makeTestTrace("shared", now.Add(time.Minute).UnixMilli(), "build")
	for _, backend := range []backends.CacheBackend{local, remote} {
		if err := NewTraceWriter(backend).Write(ctx, shared); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	store, err := NewTraceStore(backends.NewRemoteWrapper(local, remote), &PathResolver{
		buildsBase: localDir + "/traces/builds",
		spansBase:  localDir + "/traces/spans",
	})
	if err != nil {
		t.Fatalf("NewTraceStore failed: %v", err)
	}
	defer store.Close()

	var lastCurrent, lastTotal int
	pulled, err := store.Pull(ctx, func(current, total int) { lastCurrent, lastTotal = current, total })
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if pulled != 2 || lastCurrent != 2 || lastTotal != 2 {
		t.Errorf("expected the build and span file of remote-only to be pulled, got pulled=%d progress=%d/%d", pulled, lastCurrent, lastTotal)
	}

	entries, err := store.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected both traces locally after pull, got %d", len(entries))
	}
}
