import {
  countColumn,
  durationColumn,
  percentColumn,
  repeatedTimeColumn,
  repeatsColumn,
  targetColumn,
} from "../columns";
import { AppPage } from "../components/AppPage";
import { DataTable } from "../components/DataTable";
import { Panel } from "../components/Panel";
import { StatGrid } from "../components/StatGrid";
import { TrendChart } from "../components/TrendChart";
import { type Filters, rangeLabel } from "../filters";
import { formatCount, formatDuration, formatPercent } from "../links";
import { cachePhaseKeys, phases } from "../phases";
import { kpiQuery, phaseTotalsQuery, trendQuery } from "../sql/builds";
import { cacheOverheadQuery, repeatedExecutionsQuery } from "../sql/targets";
import { useSql } from "../useSql";

export function CachePage({ filters }: { filters: Filters }) {
  const kpis = useSql(kpiQuery(filters));
  const trend = useSql(trendQuery(filters));
  const totals = useSql(phaseTotalsQuery(filters));
  const repeats = useSql(repeatedExecutionsQuery(filters, 50));
  const overhead = useSql(cacheOverheadQuery(filters, 25));

  const current = kpis.rows.find((row) => Number(row.is_current) === 1) ?? {};
  const previous = kpis.rows.find((row) => Number(row.is_current) === 0);
  const firstRepeat = repeats.rows[0] ?? {};
  const overheadTotal = cachePhaseKeys.reduce(
    (sum, key) => sum + Number(totals.rows[0]?.[key] ?? 0),
    0,
  );

  return (
    <AppPage page="cache" filters={filters}>
      <Panel title="Cache health" state={kpis}>
        <StatGrid
          previousLabel={`previous ${rangeLabel(filters.range)}`}
          stats={[
            {
              label: "Target cache hit rate",
              value: formatPercent(Number(current.hit_rate)),
              current: Number(current.hit_rate),
              comparison: previous
                ? {
                    previous: Number(previous.hit_rate),
                    kind: "points",
                    higherIsBetter: true,
                  }
                : undefined,
            },
            {
              label: "Repeated executions",
              value: formatCount(Number(firstRepeat.all_repeats ?? 0)),
              hint: "Successful executions of inputs that had already executed successfully in this range",
            },
            {
              label: "Share of executions repeated",
              value: formatPercent(
                Number(firstRepeat.all_repeats ?? 0) /
                  Math.max(Number(firstRepeat.all_executions ?? 0), 1),
              ),
            },
            {
              label: "Time spent repeating",
              value: formatDuration(Number(firstRepeat.all_repeat_ms ?? 0)),
            },
            {
              label: "Cache overhead",
              value: formatDuration(overheadTotal),
              hint: `Time in ${cachePhaseKeys
                .map((key) => phases.find((phase) => phase.key === key)?.label)
                .join(", ")} across all targets`,
            },
            {
              label: "Median async cache wait",
              value: formatDuration(Number(current.async_cache_wait_p50_ms)),
              current: Number(current.async_cache_wait_p50_ms),
              comparison: previous
                ? {
                    previous: Number(previous.async_cache_wait_p50_ms),
                    kind: "relative",
                    higherIsBetter: false,
                  }
                : undefined,
              hint: "Per build, time spent at the end waiting for cache uploads to finish (grog v0.49.0+)",
            },
          ]}
        />
      </Panel>

      <Panel title="Target cache hit rate" state={trend}>
        <TrendChart
          height={200}
          rows={trend.rows}
          series={[
            {
              key: "hit_rate",
              label: "Target cache hit rate",
              unit: "percentunit",
              color: "green",
            },
          ]}
        />
      </Panel>

      <Panel
        title="Repeated executions"
        help="A successful execution whose change hash already executed successfully should have been a cache hit. Usual causes: a no-cache tag, outputs that never reach the shared cache, or eviction."
        state={repeats}
        empty="Every execution in this range had new inputs."
      >
        <DataTable
          rows={repeats.rows}
          rowKey={(row) => String(row.label)}
          sortedBy="Repeated time"
          columns={[
            targetColumn(),
            countColumn("Executions", "total_executions"),
            countColumn(
              "Distinct inputs",
              "input_hashes",
              "Distinct change hashes among the successful executions",
            ),
            repeatsColumn(),
            percentColumn(
              "Repeated",
              (row) => Number(row.repeats) / Math.max(Number(row.total_executions), 1),
            ),
            repeatedTimeColumn(),
          ]}
        />
      </Panel>

      <Panel
        title="Cache overhead by target"
        help="Time each target spends checking the cache, loading dependency outputs, loading its own cached outputs and writing to the cache"
        state={overhead}
      >
        <DataTable
          rows={overhead.rows}
          rowKey={(row) => String(row.label)}
          sortedBy="Total"
          columns={[
            targetColumn(),
            countColumn("Runs", "runs"),
            ...cachePhaseKeys.map((key) =>
              durationColumn(
                phases.find((phase) => phase.key === key)?.label ?? key,
                `total_${key}`,
              ),
            ),
            durationColumn("Total", "overhead_ms"),
          ]}
        />
      </Panel>
    </AppPage>
  );
}
