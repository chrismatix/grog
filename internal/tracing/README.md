# tracing

The `tracing` package implements persistent execution trace storage for Grog builds. It captures per-target phase-level timing data and stores traces via the existing `CacheBackend` interface, making them available for terminal analytics and dashboard export.

## Architecture

```
                                  +-----------------+
  build.go  ──►  TraceCollector  ──►  TraceStore  ──►  CacheBackend (FS/S3/GCS)
                  (assembles)        (persists)
```

### Key types

- **`TraceCollector`** (`collector.go`) — Created at the start of a build, records metadata (git info, version, platform, command type, patterns). After execution completes, `Finalize()` iterates the `CompletionMap` and target graph to assemble a `BuildTrace` proto with per-target `TargetSpan` entries.

- **`TraceStore`** (`store.go`) — Reads and writes traces via `CacheBackend`. Manages a date-partitioned storage layout and a lightweight protobuf index for fast listing.

- **Export functions** (`export.go`) — `ExportJSONL` writes traces as newline-delimited JSON (for Grafana Loki, Athena, BigQuery). `ExportOTLP` maps traces to OpenTelemetry-compatible JSON (for Grafana Tempo, Jaeger, Datadog) without requiring the OTEL SDK.

### Proto schema

Defined in `internal/proto/schema/trace.proto`:

- **`BuildTrace`** — One per `grog build/test/run` invocation. Contains build metadata, aggregate counts, critical path info, and a list of `TargetSpan` entries.
- **`TargetSpan`** — One per selected target. Includes status, cache result, and 8 phase-level timing fields for bottleneck analysis.
- **`TraceIndex`** / **`TraceIndexEntry`** — Lightweight index for listing traces without deserializing full trace data.

### Phase timing

Each `TargetSpan` captures granular phase durations (in milliseconds):

| Phase | Field | Description |
|-------|-------|-------------|
| Queue wait | `queue_wait_millis` | Time waiting in worker pool before execution |
| Hashing | `hash_duration_millis` | Time computing the target's ChangeHash |
| Cache check | `cache_check_millis` | Time checking cache + output checks + taint |
| Command | `command_duration_millis` | Shell command execution time |
| Output write | `output_write_millis` | Time writing outputs to cache |
| Output load | `output_load_millis` | Time loading cached outputs (cache hit) |
| Cache write | `cache_write_millis` | Time persisting TargetResult |
| Dep load | `dep_load_millis` | Time loading dependency outputs (minimal mode) |

These timings are recorded by instrumentation in `internal/execution/execute.go` and stored as transient fields on `model.Target`.

### Storage layout

Traces are stored under a `traces/` prefix in the cache backend:

```
traces/
  index                      # TraceIndex proto
  data/
    2026-03-30/
      <trace-id>             # BuildTrace proto
    2026-03-31/
      <trace-id>
```

The date partition enables efficient retention pruning via `TraceStore.Prune()`.

### Write path

Trace writing is **async and fire-and-forget** — it never blocks or slows down builds. After `executor.Execute()` returns in `build.go`, the trace is assembled and written in a background goroutine using `context.WithoutCancel`.

### Configuration

Opt-in via `grog.toml`:

```toml
[traces]
enabled = true
retention_days = 30
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
