#!/bin/sh

set -eu

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

jaeger_ui_port="${JAEGER_UI_PORT:-16686}"
jaeger_otlp_port="${JAEGER_OTLP_PORT:-4318}"
tempo_otlp_port="${TEMPO_OTLP_PORT:-4320}"
tempo_api_port="${TEMPO_API_PORT:-3200}"
loki_port="${LOKI_PORT:-3100}"

if [ -n "${GROG_BIN:-}" ]; then
  grog_mode="custom"
elif command -v grog >/dev/null 2>&1; then
  grog_mode="installed"
else
  grog_mode="go_run"
fi

run_grog() {
  if [ "$grog_mode" = "custom" ]; then
    "${GROG_BIN}" "$@"
    return
  fi

  if [ "$grog_mode" = "installed" ]; then
    grog "$@"
    return
  fi

  GOCACHE="${GOCACHE:-/tmp/grog-tracing-example-gocache}" go run "${repo_root}" "$@"
}

wait_for_http() {
  url="$1"
  description="$2"

  attempts=0
  while :; do
    status_code="$(curl -sS -o /dev/null -w '%{http_code}' "$url" || true)"
    if [ "$status_code" = "200" ]; then
      return 0
    fi

    attempts=$((attempts + 1))
    if [ "$attempts" -ge 60 ]; then
      echo "Timed out waiting for ${description} at ${url}" >&2
      return 1
    fi

    sleep 1
  done
}

wait_for_http "http://localhost:${jaeger_ui_port}" "Jaeger UI"
wait_for_http "http://localhost:${tempo_api_port}/ready" "Tempo"
wait_for_http "http://localhost:${loki_port}/ready" "Loki"

cd "$script_dir"

echo "Seeding traces from ${script_dir}"

run_grog build //:publish_demo_bundle
run_grog test //:publish_demo_bundle_test
run_grog build //:correlate_hotspots
run_grog build //...
run_grog test //...

rm -f /tmp/traces-otel.json /tmp/grog-traces.jsonl

run_grog traces export --format=otel --limit 2 --output /tmp/traces-otel.json
curl -sS -X POST "http://localhost:${jaeger_otlp_port}/v1/traces" \
  -H "Content-Type: application/json" \
  --data-binary @/tmp/traces-otel.json >/dev/null
curl -sS -X POST "http://localhost:${tempo_otlp_port}/v1/traces" \
  -H "Content-Type: application/json" \
  --data-binary @/tmp/traces-otel.json >/dev/null

run_grog traces export --format=jsonl --limit 2 >> /tmp/grog-traces.jsonl

echo "Seeded traces into Jaeger, Tempo, and Loki."
echo "Jaeger:  http://localhost:${jaeger_ui_port}"
echo "Grafana: http://localhost:${GRAFANA_PORT:-3300}"
