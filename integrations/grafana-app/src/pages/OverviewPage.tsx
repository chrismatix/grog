import { RadioButtonGroup } from "@grafana/ui";
import { useState } from "react";

import {
  addedTimeColumn,
  changeColumn,
  countColumn,
  criticalShareColumn,
  criticalTimeColumn,
  flakyInputsColumn,
  lastFailureColumn,
  repeatedTimeColumn,
  repeatsColumn,
  targetColumn,
} from "../columns";
import { AppPage } from "../components/AppPage";
import { BreakdownTable } from "../components/BreakdownTable";
import { DataTable } from "../components/DataTable";
import { Panel, PanelGrid } from "../components/Panel";
import { PhaseBar } from "../components/PhaseBar";
import { StatGrid } from "../components/StatGrid";
import { TrendChart } from "../components/TrendChart";
import { type Filters, rangeLabel } from "../filters";
import { appUrl, formatCount, formatDuration, formatPercent } from "../links";
import {
  type BreakdownDimension,
  breakdownDimensions,
  kpiQuery,
  phaseTotalsQuery,
  trendQuery,
} from "../sql/builds";
import type { Row } from "../sql/client";
import { flakyTargetsQuery, repeatedExecutionsQuery, targetsQuery } from "../sql/targets";
import { useSql } from "../useSql";

const shortList = 5;

export function buildStats(current: Row, previous: Row | undefined) {
  const comparison = (key: string, kind: "relative" | "points", higherIsBetter?: boolean) =>
    previous && Number(previous.builds) > 0
      ? { previous: Number(previous[key]), kind, higherIsBetter }
      : undefined;
  const value = (key: string) => Number(current[key] ?? Number.NaN);
  return [
    {
      label: "Builds",
      value: formatCount(value("builds")),
      current: value("builds"),
      comparison: comparison("builds", "relative"),
    },
    {
      label: "Median duration",
      value: formatDuration(value("duration_p50_ms")),
      current: value("duration_p50_ms"),
      comparison: comparison("duration_p50_ms", "relative", false),
    },
    {
      label: "p95 duration",
      value: formatDuration(value("duration_p95_ms")),
      current: value("duration_p95_ms"),
      comparison: comparison("duration_p95_ms", "relative", false),
    },
    {
      label: "Median critical path",
      value: formatDuration(value("critical_path_p50_ms")),
      current: value("critical_path_p50_ms"),
      comparison: comparison("critical_path_p50_ms", "relative", false),
      hint: "Execution time along the longest dependency chain (grog v0.49.0+)",
    },
    {
      label: "Target cache hit rate",
      value: formatPercent(value("hit_rate")),
      current: value("hit_rate"),
      comparison: comparison("hit_rate", "points", true),
    },
    {
      label: "Builds with failures",
      value: formatPercent(value("failure_rate")),
      current: value("failure_rate"),
      comparison: comparison("failure_rate", "points", false),
    },
    {
      label: "Rerun builds",
      value: formatPercent(value("rerun_rate")),
      current: value("rerun_rate"),
      comparison: comparison("rerun_rate", "points", false),
      hint: "Builds from a GitHub run attempt above 1: someone pressed re-run",
    },
    {
      label: "Total build time",
      value: formatDuration(value("total_ms")),
      current: value("total_ms"),
      comparison: comparison("total_ms", "relative", false),
    },
  ];
}

export function OverviewPage({ filters }: { filters: Filters }) {
  const [dimension, setDimension] = useState<BreakdownDimension>("job");
  const kpis = useSql(kpiQuery(filters));
  const trend = useSql(trendQuery(filters));
  const phases = useSql(phaseTotalsQuery(filters));
  const regressions = useSql(targetsQuery(filters, "slower", shortList));
  const flaky = useSql(flakyTargetsQuery(filters, shortList));
  const repeats = useSql(repeatedExecutionsQuery(filters, shortList));
  const hotspots = useSql(targetsQuery(filters, "critical", shortList));

  const current = kpis.rows.find((row) => Number(row.is_current) === 1);
  const previous = kpis.rows.find((row) => Number(row.is_current) === 0);
  const flakyTotal = Number(flaky.rows[0]?.all_flaky_targets ?? 0);
  const repeatTotal = Number(repeats.rows[0]?.all_repeat_ms ?? 0);

  return (
    <AppPage page="" filters={filters}>
      <Panel title="Health" state={kpis}>
        {current && (
          <StatGrid
            stats={buildStats(current, previous)}
            previousLabel={`previous ${rangeLabel(filters.range)}`}
          />
        )}
      </Panel>

      <PanelGrid>
        <Panel title="Build duration" state={trend}>
          <TrendChart
            rows={trend.rows}
            series={[
              { key: "duration_p50_ms", label: "Median", unit: "ms" },
              { key: "duration_p95_ms", label: "p95", unit: "ms" },
              {
                key: "critical_path_p50_ms",
                label: "Median critical path",
                unit: "ms",
              },
            ]}
          />
        </Panel>
        <Panel title="Cache and failures" state={trend}>
          <TrendChart
            rows={trend.rows}
            series={[
              {
                key: "hit_rate",
                label: "Target cache hit rate",
                unit: "percentunit",
                color: "green",
              },
              {
                key: "failure_rate",
                label: "Builds with failures",
                unit: "percentunit",
                color: "red",
              },
              {
                key: "rerun_rate",
                label: "Rerun builds",
                unit: "percentunit",
                color: "orange",
              },
            ]}
          />
        </Panel>
      </PanelGrid>

      <h3>Needs attention</h3>
      <PanelGrid minimumWidth={560}>
        <Panel
          title="Biggest regressions"
          help={`Targets whose median command time grew most against the previous ${rangeLabel(filters.range)}, weighted by how often they ran`}
          viewAll={appUrl("regressions")}
          state={regressions}
          empty="No target got meaningfully slower."
        >
          <DataTable
            rows={regressions.rows}
            rowKey={(row) => String(row.label)}
            sortedBy="Added time"
            columns={[targetColumn(), changeColumn(), addedTimeColumn()]}
          />
        </Panel>
        <Panel
          title="Critical-path hotspots"
          help="Targets that hold builds up the most: where speeding up or caching pays off directly"
          viewAll={appUrl("targets", { sort: "critical" })}
          state={hotspots}
          empty="No critical-path data in this range (needs grog v0.49.0+)."
        >
          <DataTable
            rows={hotspots.rows}
            rowKey={(row) => String(row.label)}
            sortedBy="Critical path / build"
            columns={[targetColumn(), criticalTimeColumn(), criticalShareColumn()]}
          />
        </Panel>
        <Panel
          title={`Repeated executions · ${formatDuration(repeatTotal)} wasted`}
          help="Targets that re-ran inputs which had already run successfully: the cache should have hit"
          viewAll={appUrl("cache")}
          state={repeats}
          empty="Every execution had new inputs."
        >
          <DataTable
            rows={repeats.rows}
            rowKey={(row) => String(row.label)}
            sortedBy="Repeated time"
            columns={[targetColumn(), repeatsColumn(), repeatedTimeColumn()]}
          />
        </Panel>
        <Panel
          title={`Flaky targets · ${formatCount(flakyTotal)}`}
          help="Targets that both failed and passed with identical inputs"
          viewAll={appUrl("reliability")}
          state={flaky}
          empty="No flaky targets in this range."
        >
          <DataTable
            rows={flaky.rows}
            rowKey={(row) => String(row.label)}
            sortedBy="Flaky inputs"
            columns={[
              targetColumn(),
              flakyInputsColumn(),
              countColumn("Failures", "total_failures"),
              lastFailureColumn(),
            ]}
          />
        </Panel>
      </PanelGrid>

      <Panel
        title="Where target time goes"
        help="Summed over every target in every matching build. Queue is time a ready target waited for a worker."
        state={phases}
      >
        <PhaseBar
          totals={Object.fromEntries(
            Object.entries(phases.rows[0] ?? {}).map(([key, value]) => [key, Number(value)]),
          )}
        />
      </Panel>

      <Panel
        title="Breakdown"
        actions={
          <RadioButtonGroup
            size="sm"
            value={dimension}
            options={Object.entries(breakdownDimensions).map(([value, entry]) => ({
              label: entry.label,
              value: value as BreakdownDimension,
            }))}
            onChange={setDimension}
          />
        }
      >
        <BreakdownTable filters={filters} dimension={dimension} />
      </Panel>
    </AppPage>
  );
}
