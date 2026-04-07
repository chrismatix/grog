# Tracing Integrations

This example runs Jaeger, Grafana Tempo, and Grafana Loki locally via
Docker Compose so you can see Grog execution traces in each tool. It also
includes a tiny synthetic Grog workspace with dummy targets that generate
trace-friendly delays and fake "insight" artifacts.

## Prerequisites

- Docker and Docker Compose
- Grog with tracing enabled
- `curl` and `jq`

## Quick start

1. Start the services:

```bash
docker compose up -d
```

If any of the default ports are already in use, override them when starting the
stack:

```bash
GRAFANA_PORT=4300 docker compose up -d
```

2. Seed the example workspace and import the traces:

```bash
sh ./seed.sh
```

The included [`grog.toml`](/Users/christophproschel/codingprojects/grog/examples/tracing/grog.toml)
already enables tracing and pins `num_workers = 2` so the sample workload has a
better chance of showing queueing and parallelism.

The dummy targets create files under `out/`, `reports/`, and `dist/`, with
deterministic pseudo-random waits based on the input content. That makes the
exported traces look more realistic without making the example flaky.

`seed.sh` waits for Jaeger, Tempo, and Loki to become ready, runs the synthetic
`grog build` and `grog test` commands, exports the two most recent traces, posts
OTLP data to Jaeger and Tempo, and appends JSONL traces for Loki.

3. If you want to run the steps manually instead:

```bash
# Jaeger and Tempo (OTLP)
grog traces export --format=otel --output /tmp/traces-otel.json
curl -X POST http://localhost:4318/v1/traces \
  -H "Content-Type: application/json" \
  -d @/tmp/traces-otel.json

curl -X POST http://localhost:4320/v1/traces \
  -H "Content-Type: application/json" \
  -d @/tmp/traces-otel.json

# Loki (JSONL via promtail log file)
grog traces export --format=jsonl >> /tmp/grog-traces.jsonl
```

4. Open the UIs:

| Service | URL |
|---------|-----|
| Jaeger | http://localhost:16686 |
| Grafana (Tempo + Loki) | http://localhost:3300 |

In Jaeger, search for service `grog` to find build traces.

In Grafana, the Tempo and Loki datasources are pre-configured. Use
**Explore** to query traces in Tempo or log lines in Loki.

Grafana also provisions a starter dashboard named **Grog Tracing Overview**
inside the **Tracing Example** folder. After running `sh ./seed.sh`, it shows:

- latest build duration, cache hits, and failures
- duration trends from the seeded synthetic traces
- cache hits versus total targets
- the raw seeded trace events from Loki

## Services

| Container | Port | Purpose |
|-----------|------|---------|
| jaeger | 16686 (UI), 4318 (OTLP HTTP) | Trace visualization |
| tempo | 4320 (OTLP HTTP), 3200 (API) | Trace storage for Grafana |
| loki | 3100 | Log aggregation |
| promtail | - | Tails JSONL log file into Loki |
| grafana | 3300 | Dashboards for Tempo and Loki |

The published host ports are configurable via `JAEGER_UI_PORT`,
`JAEGER_OTLP_PORT`, `TEMPO_OTLP_PORT`, `TEMPO_API_PORT`, `LOKI_PORT`, and
`GRAFANA_PORT`.

## Cleanup

```bash
docker compose down -v
```
