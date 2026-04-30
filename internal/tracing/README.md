# tracing

The `tracing` package implements persistent execution trace storage for Grog builds. It captures per-target phase-level timing data, stores traces as Parquet files via the existing `CacheBackend` interface, and queries them using DuckDB (via the Go driver) for terminal analytics and dashboard export.

## Architecture

```
Write path (pure Go, no DuckDB):
  build.go  ──►  TraceCollector  ──►  TraceWriter  ──►  CacheBackend (FS/S3/GCS)
                  (assembles)          (parquet-go)

Query path (DuckDB Go driver):
  CLI command  ──►  TraceStore  ──►  database/sql + go-duckdb
                                     ──►  read_parquet(glob)
```

### Key types

- **`TraceCollector`** (`collector.go`) — Created at the start of a build, records metadata (git info, version, platform, command type, patterns). After execution completes, `Finalize()` iterates the `CompletionMap` and target graph to assemble a `*BuildTrace` with `BuildRow` + `[]SpanRow`.

- **`TraceWriter`** (`store.go`) — Write-only. Serializes traces as Parquet files using `parquet-go` and persists via `CacheBackend`. Used from the async build goroutine. Does not require DuckDB.

- **`TraceStore`** (`store.go`) — Full read+write. Wraps `TraceWriter` and adds query methods (`List`, `FindAndLoad`, `Stats`, `DetailedStats`, `Prune`) that run SQL via the DuckDB Go driver (`database/sql` + `go-duckdb`).

- **`PathResolver`** (`path_resolver.go`) — Constructs DuckDB-readable glob paths from config (local FS paths, `s3://`, or `gcs://` URLs).

- **Export functions** (`export.go`) — `ExportJSONL` writes traces as newline-delimited JSON (for Grafana Loki, Athena, BigQuery). `ExportOTLP` maps traces to OpenTelemetry-compatible JSON (for Grafana Tempo, Jaeger, Datadog) without requiring the OTEL SDK.

### Data model

Defined as Go structs in `schema.go` with `parquet` struct tags:

- **`BuildRow`** — One row per `grog build/test/run` invocation. Contains build metadata, aggregate counts, and critical path info.
- **`SpanRow`** — One row per selected target. Includes status, cache result, and 8 phase-level timing fields for bottleneck analysis.
- **`BuildTrace`** — In-memory container holding `BuildRow` + `[]SpanRow`.

### Phase timing

Each `TargetSpan` captures granular phase durations (in milliseconds):

| Phase        | Field                     | Description                                    |
| ------------ | ------------------------- | ---------------------------------------------- |
| Queue wait   | `queue_wait_millis`       | Time waiting in worker pool before execution   |
| Hashing      | `hash_duration_millis`    | Time computing the target's ChangeHash         |
| Cache check  | `cache_check_millis`      | Time checking cache + output checks + taint    |
| Command      | `command_duration_millis` | Shell command execution time                   |
| Output write | `output_write_millis`     | Time writing outputs to cache                  |
| Output load  | `output_load_millis`      | Time loading cached outputs (cache hit)        |
| Cache write  | `cache_write_millis`      | Time persisting TargetResult                   |
| Dep load     | `dep_load_millis`         | Time loading dependency outputs (minimal mode) |

These timings are recorded by instrumentation in `internal/execution/execute.go` and stored as transient fields on `model.Target`.

### Storage layout

Traces are stored as Parquet files under a `traces/` prefix in the cache backend — two tables, date-partitioned, one file per trace:

```
traces/
  builds/
    2026-03-30/
      <trace-id>.parquet     # 1 row (BuildRow)
    2026-03-31/
      <trace-id>.parquet
  spans/
    2026-03-30/
      <trace-id>.parquet     # N rows (SpanRow, one per target)
```

No index file. DuckDB scans Parquet files directly via `read_parquet('traces/builds/**/*.parquet')`.

### Write path

Trace writing is **async and fire-and-forget** — it never blocks or slows down builds. After `executor.Execute()` returns in `build.go`, the trace is assembled and written in a background goroutine using `context.WithoutCancel`. The write path uses `parquet-go` (pure Go) and does not require DuckDB.

### Query path

All query operations (`list`, `show`, `stats`, `prune`) use the DuckDB Go driver (`github.com/marcboeker/go-duckdb`) via `database/sql`. DuckDB reads Parquet files directly from the filesystem, S3, or GCS. The `PathResolver` constructs the appropriate glob paths based on the configured storage backend.

### Configuration

Opt-in via `grog.toml`:

```toml
[traces]
enabled = true
```

Optionally uses a separate storage backend from the build cache:

```toml
[traces]
enabled = true
backend = "s3"

[traces.s3]
bucket = "my-traces-bucket"
prefix = "grog-traces"
```
