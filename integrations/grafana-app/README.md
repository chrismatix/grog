# Grog CI (Grafana app)

CI health and bottleneck analysis over the grog build traces that `grog traces export --format=otel` sends to ClickHouse
through an OpenTelemetry collector (`ServiceName = 'grog'`). Every tab shares one filter bar (range, workflow, job,
branch, arch, any `grog.tag.*`), and every number compares the selected range with the equally long range before it.

- **Overview** — health stats, duration and cache/failure trends, and a "needs attention" summary of the tabs below.
- **Regressions** — targets whose median command time moved most, ranked by the CI time the change added.
- **Targets** — every target with cache hit rate, medians, and how much it holds up the critical path.
- **Cache** — executions that re-ran inputs which had already run (the cache should have hit), and cache overhead.
- **Reliability** — flaky targets (same inputs both failed and passed), failing targets and reruns.
- **Builds** — the build list; tick two builds to compare them target by target.
- **CI jobs** — GitHub Actions queue time and steps around grog, from the optional `github-actions` job spans below.

A build page shows the dependency graph, timeline, per-phase totals and a what-if estimate per critical-path target.

## Requirements

- Grafana 12 or later with the [ClickHouse datasource](https://grafana.com/grafana/plugins/grafana-clickhouse-datasource/).
- An OpenTelemetry collector with the [ClickHouse exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/clickhouseexporter),
  which writes spans to `otel_traces` in the `default` database unless configured otherwise.
- grog v0.49.0 or later: dependency edges, the critical path and the `grog.tag.github.*` attributes need it.

## Exporting traces

After each CI build, post the build's trace to the collector's OTLP/HTTP endpoint:

```bash
grog traces export --format=otel --limit 1 |
  curl --fail --header "Content-Type: application/json" --data-binary @- "$OTLP_ENDPOINT/v1/traces"
```

On GitHub Actions grog tags every trace with the run context (`grog.tag.github.*`), which the workflow, job and branch
filters use.

### GitHub job spans (optional)

The **CI jobs** tab and the job panel on a build page read spans with `service.name = github-actions`.
[`examples/github-job-spans.sh`](examples/github-job-spans.sh) prints the running job and its finished steps as OTLP/JSON;
run it as the last step of a job with `jq` installed and the workflow granted `actions: read`:

```yaml
permissions:
  actions: read
steps:
  # ... checkout, grog build, trace export
  - name: Export job spans
    if: always()
    env:
      GITHUB_TOKEN: ${{ github.token }}
    run: |
      ./github-job-spans.sh |
        curl --fail --header "Content-Type: application/json" --data-binary @- "$OTLP_ENDPOINT/v1/traces"
```

## Install

The plugin is unsigned, so Grafana has to allow it explicitly:

```bash
npm ci && npm run build
cp -r dist /var/lib/grafana/plugins/chrismatix-grog-app
GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=chrismatix-grog-app grafana server
```

Then open **Administration → Plugins → Grog CI**, enable it, pick the ClickHouse datasource and, if the collector writes
elsewhere, the traces table (for example `otel.otel_traces`).

## Development

`npm run dev` rebuilds `dist/` on change and `npm run typecheck` runs `tsc`. Bump `version` in `plugin.json` with every
release: browsers cache `module.js` per plugin version.
