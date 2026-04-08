package tracing

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"grog/internal/caching/backends"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/parquet-go/parquet-go"
)

const (
	tracesBuildsPath = "traces/builds"
	tracesSpansPath  = "traces/spans"
)

// TraceWriter writes traces as Parquet files via CacheBackend.
// It does not require DuckDB and is safe to use from the async build goroutine.
type TraceWriter struct {
	backend backends.CacheBackend
}

func NewTraceWriter(backend backends.CacheBackend) *TraceWriter {
	return &TraceWriter{backend: backend}
}

func dateStr(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// Write persists a BuildTrace as two Parquet files (builds + spans).
func (w *TraceWriter) Write(ctx context.Context, trace *BuildTrace) error {
	traceDate := time.UnixMilli(trace.Build.StartTimeUnixMillis)
	date := dateStr(traceDate)
	key := trace.Build.TraceID + ".parquet"

	// Write builds Parquet
	buildsPath := fmt.Sprintf("%s/%s", tracesBuildsPath, date)
	buildsBuf, err := writeParquet([]BuildRow{trace.Build})
	if err != nil {
		return fmt.Errorf("write builds parquet: %w", err)
	}
	if err := w.backend.Set(ctx, buildsPath, key, bytes.NewReader(buildsBuf)); err != nil {
		return fmt.Errorf("store builds parquet: %w", err)
	}

	// Write spans Parquet
	if len(trace.Spans) > 0 {
		spansPath := fmt.Sprintf("%s/%s", tracesSpansPath, date)
		spansBuf, err := writeParquet(trace.Spans)
		if err != nil {
			return fmt.Errorf("write spans parquet: %w", err)
		}
		if err := w.backend.Set(ctx, spansPath, key, bytes.NewReader(spansBuf)); err != nil {
			return fmt.Errorf("store spans parquet: %w", err)
		}
	}

	return nil
}

func writeParquet[T any](rows []T) ([]byte, error) {
	var buf bytes.Buffer
	writer := parquet.NewGenericWriter[T](&buf)
	if _, err := writer.Write(rows); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TraceStore provides both write and query capabilities for traces.
// Query methods use DuckDB via database/sql.
type TraceStore struct {
	writer   *TraceWriter
	resolver *PathResolver
	db       *sql.DB
}

// NewTraceStore creates a full store with write + query support.
func NewTraceStore(backend backends.CacheBackend, resolver *PathResolver) (*TraceStore, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}

	return &TraceStore{
		writer:   NewTraceWriter(backend),
		resolver: resolver,
		db:       db,
	}, nil
}

// Close releases the DuckDB connection.
func (s *TraceStore) Close() error {
	return s.db.Close()
}

// Write delegates to the TraceWriter.
func (s *TraceStore) Write(ctx context.Context, trace *BuildTrace) error {
	return s.writer.Write(ctx, trace)
}

// Pull downloads remote trace files that are not yet in the local cache.
// It lists keys from the remote backend and triggers a Get() for each missing
// file, which causes the RemoteWrapper to fetch and cache it locally.
// Returns the number of files synced.
// PullProgress is called during Pull to report progress.
// current is the number of files processed so far, total is the total number of
// remote files to process.
type PullProgress func(current, total int)

func (s *TraceStore) Pull(ctx context.Context, onProgress PullProgress) (int, error) {
	// First pass: collect all remote keys and filter to those needing sync.
	type pullItem struct {
		subPath  string
		fileName string
	}
	var items []pullItem

	for _, table := range []string{tracesBuildsPath, tracesSpansPath} {
		remoteKeys, err := s.writer.backend.ListKeys(ctx, table, ".parquet")
		if err != nil {
			return 0, fmt.Errorf("list remote keys for %s: %w", table, err)
		}

		for _, key := range remoteKeys {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) != 2 {
				continue
			}
			subPath := fmt.Sprintf("%s/%s", table, parts[0])
			fileName := parts[1]

			localExists := false
			if rw, ok := s.writer.backend.(*backends.RemoteWrapper); ok {
				localExists, _ = rw.GetFS().Exists(ctx, subPath, fileName)
			} else {
				localExists, _ = s.writer.backend.Exists(ctx, subPath, fileName)
			}
			if !localExists {
				items = append(items, pullItem{subPath: subPath, fileName: fileName})
			}
		}
	}

	total := len(items)
	if onProgress != nil {
		onProgress(0, total)
	}

	synced := 0
	for i, item := range items {
		reader, err := s.writer.backend.Get(ctx, item.subPath, item.fileName)
		if err == nil {
			reader.Close()
			synced++
		}
		if onProgress != nil {
			onProgress(i+1, total)
		}
	}

	return synced, nil
}

// List returns recent builds matching optional filters.
func (s *TraceStore) List(ctx context.Context, opts ListOptions) ([]BuildRow, error) {
	var conditions []string
	if opts.Since != nil {
		conditions = append(conditions, fmt.Sprintf("start_time_unix_millis >= %d", opts.Since.UnixMilli()))
	}
	if opts.Command != "" {
		conditions = append(conditions, fmt.Sprintf("command = '%s'", sanitize(opts.Command)))
	}
	if opts.FailuresOnly {
		conditions = append(conditions, "failure_count > 0")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := opts.Limit
	if limit < 0 {
		limit = 20
	}

	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", limit)
	}

	query := fmt.Sprintf(`SELECT trace_id, workspace, git_commit, git_branch, grog_version, platform,
		command, start_time_unix_millis, total_duration_millis, total_targets,
		success_count, failure_count, cache_hit_count,
		critical_path_exec_millis, critical_path_cache_millis, async_cache_wait_millis,
		is_ci, requested_patterns
		FROM read_parquet('%s', union_by_name=true)
		%s ORDER BY start_time_unix_millis DESC %s`,
		s.resolver.BuildsGlob(), where, limitClause)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		if isNoFilesError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list traces: %w", err)
	}
	defer rows.Close()

	return scanBuildRows(rows)
}

// ListOptions controls filtering for List.
type ListOptions struct {
	Limit        int
	Since        *time.Time
	Command      string
	FailuresOnly bool
}

// LoadBuild retrieves a single build by trace ID prefix.
func (s *TraceStore) LoadBuild(ctx context.Context, traceIDPrefix string) (*BuildRow, error) {
	query := fmt.Sprintf(`SELECT trace_id, workspace, git_commit, git_branch, grog_version, platform,
		command, start_time_unix_millis, total_duration_millis, total_targets,
		success_count, failure_count, cache_hit_count,
		critical_path_exec_millis, critical_path_cache_millis, async_cache_wait_millis,
		is_ci, requested_patterns
		FROM read_parquet('%s', union_by_name=true)
		WHERE starts_with(trace_id, '%s')`,
		s.resolver.BuildsGlob(), sanitize(traceIDPrefix))

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		if isNoFilesError(err) {
			return nil, fmt.Errorf("no trace found matching %q", traceIDPrefix)
		}
		return nil, err
	}
	defer rows.Close()

	builds, err := scanBuildRows(rows)
	if err != nil {
		return nil, err
	}

	if len(builds) == 0 {
		return nil, fmt.Errorf("no trace found matching %q", traceIDPrefix)
	}
	if len(builds) > 1 {
		return nil, fmt.Errorf("ambiguous trace ID prefix %q: matches %s and %s",
			traceIDPrefix, builds[0].TraceID, builds[1].TraceID)
	}
	return &builds[0], nil
}

// LoadSpans retrieves all spans for a given trace ID.
func (s *TraceStore) LoadSpans(ctx context.Context, traceID string) ([]SpanRow, error) {
	query := fmt.Sprintf(`SELECT trace_id, label, package, change_hash, output_hash,
		status, cache_result, command, exit_code, is_test,
		start_time_unix_millis, end_time_unix_millis, total_duration_millis,
		queue_wait_millis, hash_duration_millis, cache_check_millis,
		command_duration_millis, output_write_millis, output_load_millis,
		cache_write_millis, dep_load_millis, tags, dependencies
		FROM read_parquet('%s', union_by_name=true)
		WHERE trace_id = '%s'
		ORDER BY total_duration_millis DESC`,
		s.resolver.SpansGlob(), sanitize(traceID))

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		if isNoFilesError(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	return scanSpanRows(rows)
}

// FindAndLoad retrieves a full trace by ID prefix.
func (s *TraceStore) FindAndLoad(ctx context.Context, traceIDPrefix string) (*BuildTrace, error) {
	build, err := s.LoadBuild(ctx, traceIDPrefix)
	if err != nil {
		return nil, err
	}

	spans, err := s.LoadSpans(ctx, build.TraceID)
	if err != nil {
		return nil, err
	}

	return &BuildTrace{Build: *build, Spans: spans}, nil
}

// Stats returns aggregate statistics over recent traces.
type TraceStats struct {
	TraceCount   int
	AvgDuration  float64
	CacheHitRate float64
	TotalFails   int
}

// StatsOptions controls filtering for Stats and Bottlenecks.
type StatsOptions struct {
	Limit   int
	Command string
	IsCI    *bool
}

func (s *TraceStore) Stats(ctx context.Context, opts StatsOptions) (*TraceStats, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	var conditions []string
	if opts.Command != "" {
		conditions = append(conditions, fmt.Sprintf("command = '%s'", sanitize(opts.Command)))
	}
	if opts.IsCI != nil {
		conditions = append(conditions, fmt.Sprintf("is_ci = %t", *opts.IsCI))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`SELECT
		COUNT(*) as n,
		AVG(total_duration_millis) as avg_ms,
		SUM(cache_hit_count)::FLOAT / NULLIF(SUM(total_targets), 0) * 100 as hit_pct,
		COALESCE(SUM(failure_count), 0) as fails
		FROM (SELECT * FROM read_parquet('%s', union_by_name=true)
			%s
			ORDER BY start_time_unix_millis DESC LIMIT %d)`,
		s.resolver.BuildsGlob(), where, limit)

	row := s.db.QueryRowContext(ctx, query)
	var stats TraceStats
	var avgMs, hitPct sql.NullFloat64
	if err := row.Scan(&stats.TraceCount, &avgMs, &hitPct, &stats.TotalFails); err != nil {
		if isNoFilesError(err) {
			return &TraceStats{}, nil
		}
		return nil, err
	}
	stats.AvgDuration = avgMs.Float64
	stats.CacheHitRate = hitPct.Float64
	return &stats, nil
}

// TargetBottleneck holds per-target aggregated metrics for bottleneck analysis.
type TargetBottleneck struct {
	Label          string
	Count          int
	Frequency      float64 // fraction of builds that include this target (0-1)
	Impact         float64 // avg_cmd * frequency — weighted importance score
	AvgCmd         float64
	AvgQueue       float64
	AvgIO          float64
	AvgHash        float64
	MissRate       float64
	Failures       int
	AvgOutputWrite float64
	AvgOutputLoad  float64
	AvgCacheWrite  float64
}

// BottleneckReport categorizes targets by bottleneck type.
type BottleneckReport struct {
	SlowestTargets []TargetBottleneck
	QueueSaturated []TargetBottleneck
	IOBottlenecks  []TargetBottleneck
	SlowHashing    []TargetBottleneck
	FrequentMisses []TargetBottleneck
	FlakyTargets   []TargetBottleneck

	OverallCacheMissRate float64
}

// Bottleneck thresholds
const (
	queueSaturationThresholdMs = 500
	ioBottleneckThresholdMs    = 1000
	slowHashThresholdMs        = 200
	missRateExcessPct          = 30 // miss rate must exceed overall avg by this many pp
	maxBottlenecksPerCategory  = 10
)

func (s *TraceStore) Bottlenecks(ctx context.Context, opts StatsOptions) (*BottleneckReport, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	var conditions []string
	if opts.Command != "" {
		conditions = append(conditions, fmt.Sprintf("command = '%s'", sanitize(opts.Command)))
	}
	if opts.IsCI != nil {
		conditions = append(conditions, fmt.Sprintf("is_ci = %t", *opts.IsCI))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Subquery: get the trace IDs and count of recent builds
	query := fmt.Sprintf(`WITH recent_builds AS (
			SELECT trace_id FROM read_parquet('%s', union_by_name=true)
			%s
			ORDER BY start_time_unix_millis DESC LIMIT %d
		),
		build_count AS (SELECT COUNT(*) as total FROM recent_builds)
		SELECT
			label,
			COUNT(*) as n,
			COUNT(*)::FLOAT / (SELECT total FROM build_count) as frequency,
			AVG(command_duration_millis) * COUNT(*)::FLOAT / (SELECT total FROM build_count) as impact,
			AVG(command_duration_millis) as avg_cmd,
			AVG(queue_wait_millis) as avg_queue,
			AVG(output_write_millis + output_load_millis + cache_write_millis) as avg_io,
			AVG(hash_duration_millis) as avg_hash,
			SUM(CASE WHEN cache_result = 'CACHE_MISS' THEN 1 ELSE 0 END)::FLOAT / COUNT(*) * 100 as miss_rate,
			SUM(CASE WHEN status = 'FAILURE' THEN 1 ELSE 0 END) as failures,
			AVG(output_write_millis) as avg_output_write,
			AVG(output_load_millis) as avg_output_load,
			AVG(cache_write_millis) as avg_cache_write
		FROM read_parquet('%s', union_by_name=true)
		WHERE trace_id IN (SELECT trace_id FROM recent_builds)
		GROUP BY label
		HAVING n > 1
		ORDER BY impact DESC`,
		s.resolver.BuildsGlob(), where, limit, s.resolver.SpansGlob())

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		if isNoFilesError(err) {
			return &BottleneckReport{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var all []TargetBottleneck
	var totalMissRate float64
	for rows.Next() {
		var t TargetBottleneck
		if err := rows.Scan(&t.Label, &t.Count, &t.Frequency, &t.Impact,
			&t.AvgCmd, &t.AvgQueue, &t.AvgIO, &t.AvgHash, &t.MissRate, &t.Failures,
			&t.AvgOutputWrite, &t.AvgOutputLoad, &t.AvgCacheWrite); err != nil {
			return nil, err
		}
		all = append(all, t)
		totalMissRate += t.MissRate
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	report := &BottleneckReport{}
	if len(all) > 0 {
		report.OverallCacheMissRate = totalMissRate / float64(len(all))
	}

	missThreshold := report.OverallCacheMissRate + missRateExcessPct

	for _, t := range all {
		if len(report.SlowestTargets) < maxBottlenecksPerCategory {
			report.SlowestTargets = append(report.SlowestTargets, t)
		}
		if t.AvgQueue > queueSaturationThresholdMs && len(report.QueueSaturated) < maxBottlenecksPerCategory {
			report.QueueSaturated = append(report.QueueSaturated, t)
		}
		if t.AvgIO > ioBottleneckThresholdMs && len(report.IOBottlenecks) < maxBottlenecksPerCategory {
			report.IOBottlenecks = append(report.IOBottlenecks, t)
		}
		if t.AvgHash > slowHashThresholdMs && len(report.SlowHashing) < maxBottlenecksPerCategory {
			report.SlowHashing = append(report.SlowHashing, t)
		}
		if t.MissRate > missThreshold && len(report.FrequentMisses) < maxBottlenecksPerCategory {
			report.FrequentMisses = append(report.FrequentMisses, t)
		}
		if t.Failures > 0 && len(report.FlakyTargets) < maxBottlenecksPerCategory {
			report.FlakyTargets = append(report.FlakyTargets, t)
		}
	}

	// Sort secondary categories by their primary metric (query already sorts by avg_cmd)
	sortByQueue := func(a []TargetBottleneck) {
		sort.Slice(a, func(i, j int) bool { return a[i].AvgQueue > a[j].AvgQueue })
	}
	sortByIO := func(a []TargetBottleneck) {
		sort.Slice(a, func(i, j int) bool { return a[i].AvgIO > a[j].AvgIO })
	}
	sortByHash := func(a []TargetBottleneck) {
		sort.Slice(a, func(i, j int) bool { return a[i].AvgHash > a[j].AvgHash })
	}
	sortByMissRate := func(a []TargetBottleneck) {
		sort.Slice(a, func(i, j int) bool { return a[i].MissRate > a[j].MissRate })
	}
	sortByFailures := func(a []TargetBottleneck) {
		sort.Slice(a, func(i, j int) bool { return a[i].Failures > a[j].Failures })
	}

	sortByQueue(report.QueueSaturated)
	sortByIO(report.IOBottlenecks)
	sortByHash(report.SlowHashing)
	sortByMissRate(report.FrequentMisses)
	sortByFailures(report.FlakyTargets)

	return report, nil
}

// Prune deletes traces older than the given time.
func (s *TraceStore) Prune(ctx context.Context, olderThan time.Time) (int, error) {
	cutoffMillis := olderThan.UnixMilli()

	query := fmt.Sprintf(`SELECT trace_id, start_time_unix_millis
		FROM read_parquet('%s', union_by_name=true)
		WHERE start_time_unix_millis < %d`,
		s.resolver.BuildsGlob(), cutoffMillis)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		if isNoFilesError(err) {
			return 0, nil
		}
		return 0, err
	}
	defer rows.Close()

	pruned := 0
	for rows.Next() {
		var traceID string
		var startMillis int64
		if err := rows.Scan(&traceID, &startMillis); err != nil {
			return pruned, err
		}

		date := dateStr(time.UnixMilli(startMillis))
		key := traceID + ".parquet"
		_ = s.writer.backend.Delete(ctx, fmt.Sprintf("%s/%s", tracesBuildsPath, date), key)
		_ = s.writer.backend.Delete(ctx, fmt.Sprintf("%s/%s", tracesSpansPath, date), key)
		pruned++
	}

	return pruned, rows.Err()
}

// helpers

func scanBuildRows(rows *sql.Rows) ([]BuildRow, error) {
	var result []BuildRow
	for rows.Next() {
		var b BuildRow
		if err := rows.Scan(
			&b.TraceID, &b.Workspace, &b.GitCommit, &b.GitBranch, &b.GrogVersion, &b.Platform,
			&b.Command, &b.StartTimeUnixMillis, &b.TotalDurationMillis, &b.TotalTargets,
			&b.SuccessCount, &b.FailureCount, &b.CacheHitCount,
			&b.CriticalPathExecMillis, &b.CriticalPathCacheMillis, &b.AsyncCacheWaitMillis,
			&b.IsCI, &b.RequestedPatterns,
		); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func scanSpanRows(rows *sql.Rows) ([]SpanRow, error) {
	var result []SpanRow
	for rows.Next() {
		var s SpanRow
		if err := rows.Scan(
			&s.TraceID, &s.Label, &s.Package, &s.ChangeHash, &s.OutputHash,
			&s.Status, &s.CacheResult, &s.Command, &s.ExitCode, &s.IsTest,
			&s.StartTimeUnixMillis, &s.EndTimeUnixMillis, &s.TotalDurationMillis,
			&s.QueueWaitMillis, &s.HashDurationMillis, &s.CacheCheckMillis,
			&s.CommandDurationMillis, &s.OutputWriteMillis, &s.OutputLoadMillis,
			&s.CacheWriteMillis, &s.DepLoadMillis, &s.Tags, &s.Dependencies,
		); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// sanitize prevents SQL injection for string values interpolated into queries.
// Only allows alphanumeric, hyphens, underscores, and dots.
func sanitize(s string) string {
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// isNoFilesError checks if a DuckDB error is due to no matching Parquet files.
func isNoFilesError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "No files found") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "Cannot open file")
}
