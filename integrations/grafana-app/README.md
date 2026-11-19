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

- Grafana 12.3 or later with the [ClickHouse datasource](https://grafana.com/grafana/plugins/grafana-clickhouse-datasource/).
- An OpenTelemetry collector with the [ClickHouse exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/clickhouseexporter),
  which writes spans to `otel_traces` in the `default` database unless configured otherwise.
- [`grog-full`](https://grog.build/get-started/#1-install-grog) v0.49.0 or later, since `grog traces export` needs its
  DuckDB engine; dependency edges, the critical path and the `grog.tag.github.*` attributes need v0.49.0.
- Traces enabled in `grog.toml`:

  ```toml
  [traces]
  enabled = true
  ```

## Exporting traces

After each CI build, post the build's trace to the collector's OTLP/HTTP endpoint. Run the step even when the build
fails, or the Reliability tab never sees a failure:

```yaml
- name: Export grog trace
  if: always()
  run: |
    grog traces export --format=otel --limit 1 |
      curl --fail --header "Content-Type: application/json" --data-binary @- "$OTLP_ENDPOINT/v1/traces"
```

`--limit 1` sends the newest trace; a job that runs grog more than once needs a larger limit. On GitHub Actions grog tags every trace with the run context (`grog.tag.github.*`), which the workflow, job and branch
filters use.

### GitHub job spans (optional)

The **CI jobs** tab and the job panel on a build page read spans with `service.name = github-actions`.
[`examples/github-job-spans.sh`](https://github.com/chrismatix/grog/blob/main/integrations/grafana-app/examples/github-job-spans.sh)
prints the running job and its finished steps as OTLP/JSON. Run it as the last step of a job with `jq` installed and
`actions: read` granted. Naming one permission drops every permission you leave out, so keep the ones the job already
needs:

```yaml
permissions:
  contents: read
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

Build the plugin, then mount `dist/` as Grafana's plugin directory. The plugin is unsigned, so Grafana has to allow it
explicitly:

```bash
npm ci && npm run build
docker run -p 3000:3000 \
  -v "$PWD/dist:/var/lib/grafana/plugins/chrismatix-grog-app" \
  -e GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=chrismatix-grog-app \
  -e GF_PLUGINS_PREINSTALL_SYNC=grafana-clickhouse-datasource \
  grafana/grafana:12.4.0
```

On an existing install, replace the plugin directory instead of copying into it
(`rm -rf <plugins>/chrismatix-grog-app && cp -r dist <plugins>/chrismatix-grog-app`) and add the plugin id to
`allow_loading_unsigned_plugins` under `[plugins]` in `grafana.ini`.

Then open **Administration → Plugins → Grog CI**, enable it, pick the ClickHouse datasource and, if the collector writes
elsewhere, the traces table (for example `otel.otel_traces`).

## Development

`npm run dev` rebuilds `dist/` on change and `npm run typecheck` runs `tsc`. Bump `version` in `plugin.json` with every
release: browsers cache `module.js` per plugin version.
