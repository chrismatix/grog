import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { TextLink, useStyles2 } from "@grafana/ui";

import { countColumn, durationColumn, percentColumn } from "../columns";
import type { Filters } from "../filters";
import { withParams } from "../links";
import { type BreakdownDimension, breakdownDimensions, breakdownQuery } from "../sql/builds";
import type { Row } from "../sql/client";
import { mergeQueuePrefix } from "../sql/scope";
import { SqlStatus, useSql } from "../useSql";
import { DataTable } from "./DataTable";

// The URL filters that narrow the page down to one breakdown row.
function filterFor(dimension: BreakdownDimension, key: string) {
  if (dimension === "job") {
    const [workflow, job] = key.split(" / ");
    return { workflow, job };
  }
  if (dimension === "branch") {
    return key === mergeQueuePrefix
      ? { branch: key, branchMatch: undefined }
      : { branch: key, branchMatch: "equals" };
  }
  if (dimension === "event") {
    return { tag: `github.event_name=${key}` };
  }
  return { arch: key };
}

function displayName(dimension: BreakdownDimension, key: string): string {
  if (!key || key === " / ") {
    return "untagged (before grog v0.49.0)";
  }
  if (dimension === "branch" && key === mergeQueuePrefix) {
    return "merge queue (gh-readonly-queue/…)";
  }
  return key;
}

export function BreakdownTable({
  filters,
  dimension,
}: {
  filters: Filters;
  dimension: BreakdownDimension;
}) {
  const styles = useStyles2(getStyles);
  const rows = useSql(breakdownQuery(filters, dimension));
  if (rows.loading || rows.error) {
    return <SqlStatus state={rows} />;
  }
  return (
    <DataTable
      rows={rows.rows}
      rowKey={(row) => String(row.key)}
      sortedBy="Builds"
      columns={[
        {
          label: breakdownDimensions[dimension].label,
          render: (row: Row) => {
            const key = String(row.key);
            const untagged = !key || key === " / ";
            return untagged ? (
              <span className={styles.muted}>{displayName(dimension, key)}</span>
            ) : (
              <TextLink
                href={withParams(filterFor(dimension, key))}
                inline={false}
                title={`Filter to ${key}`}
              >
                {displayName(dimension, key)}
              </TextLink>
            );
          },
        },
        countColumn("Builds", "builds"),
        durationColumn("Median", "duration_p50_ms"),
        durationColumn("p95", "duration_p95_ms"),
        durationColumn("Median critical path", "critical_path_p50_ms"),
        percentColumn("Cache hit rate", (row) => Number(row.hit_rate)),
        percentColumn(
          "With failures",
          (row) => Number(row.failure_rate),
          undefined,
          (row) => (Number(row.failure_rate) > 0.2 ? styles.bad : ""),
        ),
        percentColumn("Reruns", (row) => Number(row.rerun_rate)),
        durationColumn("Total", "total_ms"),
      ]}
    />
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  muted: css({ color: theme.colors.text.secondary }),
  bad: css({ color: theme.colors.error.text }),
});
