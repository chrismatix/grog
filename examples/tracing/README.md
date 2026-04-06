# Tracing Integrations

This example runs Jaeger, Grafana Tempo, and Grafana Loki locally via
Docker Compose so you can see Grog execution traces in each tool.

## Prerequisites

- Docker and Docker Compose
- Grog with tracing enabled
- `curl` and `jq`

## Quick start

1. Start the services:

```bash
docker compose up -d
```

2. Enable tracing in your project's `grog.toml`:

```toml
[traces]
enabled = true
```

3. Run some builds to generate traces:

```bash
grog build //...
grog test //...
```

4. Export traces to each backend:

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

5. Open the UIs:

| Service | URL |
|---------|-----|
| Jaeger | http://localhost:16686 |
| Grafana (Tempo + Loki) | http://localhost:3000 |

In Jaeger, search for service `grog` to find build traces.

In Grafana, the Tempo and Loki datasources are pre-configured. Use
**Explore** to query traces in Tempo or log lines in Loki.

## Services

| Container | Port | Purpose |
|-----------|------|---------|
| jaeger | 16686 (UI), 4318 (OTLP HTTP) | Trace visualization |
| tempo | 4320 (OTLP HTTP), 3200 (API) | Trace storage for Grafana |
| loki | 3100 | Log aggregation |
| promtail | - | Tails JSONL log file into Loki |
| grafana | 3000 | Dashboards for Tempo and Loki |

## Cleanup

```bash
docker compose down -v
```
