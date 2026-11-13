import { locationService } from "@grafana/runtime";
import { RadioButtonGroup } from "@grafana/ui";
import { useMemo } from "react";

import {
  addedTimeColumn,
  changeColumn,
  countColumn,
  criticalShareColumn,
  medianColumn,
  targetColumn,
} from "../columns";
import { AppPage } from "../components/AppPage";
import { DataTable } from "../components/DataTable";
import { Panel } from "../components/Panel";
import { findStepChange, Sparkline } from "../components/Sparkline";
import { type Filters, rangeLabel } from "../filters";
import { formatDay } from "../links";
import { minimumExecutions, targetsQuery, targetTrendsQuery } from "../sql/targets";
import { useSql } from "../useSql";

export function RegressionsPage({
  filters,
  direction,
}: {
  filters: Filters;
  direction: "slower" | "faster";
}) {
  const targets = useSql(targetsQuery(filters, direction, 50));
  const labels = targets.rows.map((row) => String(row.label));
  const trends = useSql(labels.length ? targetTrendsQuery(filters, labels) : undefined);

  const series = useMemo(() => {
    const byLabel = new Map<string, Array<{ bucketMs: number; p50: number }>>();
    for (const row of trends.rows) {
      const label = String(row.label);
      byLabel.set(label, [
        ...(byLabel.get(label) ?? []),
        { bucketMs: Number(row.bucket_ms), p50: Number(row.p50_ms) },
      ]);
    }
    return new Map(
      [...byLabel.entries()].map(([label, points]) => {
        const values = points.map((point) => point.p50);
        const step = findStepChange(values);
        return [
          label,
          {
            values,
            step,
            changedAt: step !== undefined ? points[step].bucketMs : undefined,
          },
        ];
      }),
    );
  }, [trends.rows]);

  return (
    <AppPage page="regressions" filters={filters}>
      <Panel
        title={direction === "slower" ? "Targets that got slower" : "Targets that got faster"}
        help={`Median command time over the last ${rangeLabel(filters.range)} against the ${rangeLabel(filters.range)} before, for targets that executed at least ${minimumExecutions} times in both. Ranked by added time: the change in median times the number of executions.`}
        actions={
          <RadioButtonGroup
            size="sm"
            value={direction}
            options={[
              { label: "Slower", value: "slower", icon: "arrow-up" },
              { label: "Faster", value: "faster", icon: "arrow-down" },
            ]}
            onChange={(value) =>
              locationService.partial({ direction: value === "slower" ? undefined : value }, true)
            }
          />
        }
        state={targets}
        empty={`No target executed at least ${minimumExecutions} times in both periods and got ${direction}.`}
      >
        <DataTable
          rows={targets.rows}
          rowKey={(row) => String(row.label)}
          sortedBy={direction === "slower" ? "Added time" : undefined}
          columns={[
            targetColumn(),
            {
              label: "Trend",
              title: `Daily median command time over both periods; the dashed line marks the detected change`,
              render: (row) => {
                const entry = series.get(String(row.label));
                return entry ? <Sparkline values={entry.values} markIndex={entry.step} /> : null;
              },
            },
            {
              label: "Changed around",
              title: "The day that best splits the daily medians into a before and after level",
              sortValue: (row) => series.get(String(row.label))?.changedAt ?? Number.NaN,
              render: (row) => {
                const changedAt = series.get(String(row.label))?.changedAt;
                return changedAt ? formatDay(changedAt) : "—";
              },
            },
            medianColumn(),
            changeColumn(),
            countColumn("Executions", "executed_runs"),
            criticalShareColumn(),
            addedTimeColumn(),
          ]}
        />
      </Panel>
    </AppPage>
  );
}
