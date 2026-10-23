import { TextLink } from "@grafana/ui";

import type { Column } from "./components/DataTable";
import { parseTags, TargetLabel } from "./components/TargetLabel";
import { buildUrl, formatCount, formatDuration, formatPercent, formatTime } from "./links";
import type { Row } from "./sql/client";

// Column builders shared by the overview's short lists and the full tables on each tab.

export function targetColumn(): Column<Row> {
  return {
    label: "Target",
    render: (row) => <TargetLabel label={String(row.label)} tags={parseTags(row.latest_tags)} />,
  };
}

export function durationColumn(label: string, key: string, title?: string): Column<Row> {
  return {
    label,
    title,
    numeric: true,
    sortValue: (row) => Number(row[key] ?? Number.NaN),
    render: (row) => formatDuration(Number(row[key] ?? Number.NaN)),
  };
}

export function countColumn(label: string, key: string, title?: string): Column<Row> {
  return {
    label,
    title,
    numeric: true,
    sortValue: (row) => Number(row[key]),
    render: (row) => formatCount(Number(row[key])),
  };
}

export function percentColumn(
  label: string,
  value: (row: Row) => number,
  title?: string,
  className?: (row: Row) => string,
): Column<Row> {
  return {
    label,
    title,
    numeric: true,
    className,
    sortValue: value,
    render: (row) => formatPercent(value(row)),
  };
}

export function criticalTimePerBuild(row: Row): number {
  return Number(row.critical_ms) / Math.max(Number(row.builds_with_critical_path), 1);
}

export function criticalPathShare(row: Row): number {
  return Number(row.critical_path_runs) / Math.max(Number(row.builds_with_critical_path), 1);
}

export function medianChange(row: Row): number {
  return Number(row.p50_ms) - Number(row.previous_p50_ms);
}

export function medianColumn(): Column<Row> {
  return {
    label: "Median",
    numeric: true,
    title: "Previous period → selected period, executed runs only",
    sortValue: (row) => Number(row.p50_ms),
    render: (row) =>
      `${formatDuration(Number(row.previous_p50_ms))} → ${formatDuration(Number(row.p50_ms))}`,
  };
}

export function changeColumn(): Column<Row> {
  return {
    label: "Change",
    numeric: true,
    sortValue: (row) => medianChange(row) / Number(row.previous_p50_ms),
    render: (row) => {
      const change = medianChange(row);
      const relative = change / Number(row.previous_p50_ms);
      if (!Number.isFinite(relative)) {
        return "—";
      }
      return `${change > 0 ? "+" : ""}${formatDuration(change)} (${relative > 0 ? "+" : ""}${formatPercent(relative)})`;
    },
  };
}

export function addedTimeColumn(): Column<Row> {
  return durationColumn(
    "Added time",
    "impact_ms",
    "Change in median × executions this period: how much CI time the change cost (or saved)",
  );
}

export function criticalTimeColumn(): Column<Row> {
  return {
    label: "Critical path / build",
    numeric: true,
    title:
      "Average time this target adds to the critical path per build: the most a build could save if it were instant (grog v0.49.0+)",
    sortValue: criticalTimePerBuild,
    render: (row) => formatDuration(criticalTimePerBuild(row)),
  };
}

export function criticalShareColumn(): Column<Row> {
  return percentColumn(
    "On critical path",
    criticalPathShare,
    "Share of builds with critical-path data where this target is on it",
  );
}

export function lastFailureColumn(): Column<Row> {
  return {
    label: "Last failure",
    sortValue: (row) => Number(row.last_failure_ms),
    render: (row) =>
      row.last_failure_trace_id ? (
        <TextLink
          href={buildUrl(
            String(row.last_failure_trace_id),
            Number(row.last_failure_build_start_ms),
            Number(row.last_failure_build_duration_ms),
          )}
          inline={false}
        >
          {formatTime(Number(row.last_failure_ms))}
        </TextLink>
      ) : (
        "—"
      ),
  };
}

export function repeatsColumn(): Column<Row> {
  return countColumn(
    "Repeats",
    "repeats",
    "Successful executions of inputs that had already executed successfully",
  );
}

export function repeatedTimeColumn(): Column<Row> {
  return durationColumn(
    "Repeated time",
    "total_repeat_ms",
    "Command time spent re-running inputs that had already run",
  );
}

export function flakyInputsColumn(): Column<Row> {
  return countColumn("Flaky inputs", "flaky_hashes", "Change hashes that both failed and passed");
}
