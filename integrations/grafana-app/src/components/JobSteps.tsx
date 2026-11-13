import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { useStyles2, useTheme2 } from "@grafana/ui";

import { formatDuration } from "../links";
import type { Row } from "../sql/client";

// A GitHub job's queue and steps on one time axis, with the grog build drawn on top.
export function JobSteps({
  rows,
  buildStartMs,
  buildDurationMs,
}: {
  rows: Row[];
  buildStartMs: number;
  buildDurationMs: number;
}) {
  const styles = useStyles2(getStyles);
  const theme = useTheme2();
  const job = rows.find((row) => Number(row.is_job) === 1);
  if (!job) {
    return null;
  }
  const queueMs = Number(job.queue_ms);
  const origin = Number(job.start_ms) - queueMs;
  const total = Math.max(Number(job.duration_ms) + queueMs, 1);
  const bars = [
    {
      name: "Waiting for a runner",
      startMs: origin,
      durationMs: queueMs,
      color: "purple",
    },
    ...rows
      .filter((row) => Number(row.is_job) !== 1)
      .map((row) => ({
        name: String(row.name),
        startMs: Number(row.start_ms),
        durationMs: Number(row.duration_ms),
        color: Number(row.failed) ? "red" : "orange",
      })),
  ];

  return (
    <div>
      {bars.map((bar) => (
        <div key={`${bar.name}-${bar.startMs}`} className={styles.row}>
          <div className={styles.label} title={bar.name}>
            {bar.name}
          </div>
          <div className={styles.track}>
            <div
              className={styles.build}
              style={{
                left: `${((buildStartMs - origin) / total) * 100}%`,
                width: `${(buildDurationMs / total) * 100}%`,
              }}
            />
            <div
              className={styles.bar}
              style={{
                left: `${((bar.startMs - origin) / total) * 100}%`,
                width: `${Math.max((bar.durationMs / total) * 100, 0.2)}%`,
                background: theme.visualization.getColorByName(bar.color),
              }}
            />
          </div>
          <div className={styles.duration}>{formatDuration(bar.durationMs)}</div>
        </div>
      ))}
      <p className={styles.note}>
        The shaded band is this grog build. Runner: {String(job.runner_labels)}
      </p>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  row: css({
    display: "grid",
    gridTemplateColumns: "minmax(200px, 30%) 1fr 80px",
    gap: theme.spacing(1),
    alignItems: "center",
    height: 22,
    fontSize: theme.typography.bodySmall.fontSize,
    "&:hover": { background: theme.colors.action.hover },
  }),
  label: css({
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  }),
  track: css({
    position: "relative",
    height: 12,
    background: theme.colors.background.secondary,
  }),
  build: css({
    position: "absolute",
    top: -5,
    bottom: -5,
    background: theme.colors.action.selected,
  }),
  bar: css({ position: "absolute", top: 0, bottom: 0, minWidth: 2 }),
  duration: css({
    textAlign: "right",
    fontVariantNumeric: "tabular-nums",
    color: theme.colors.text.secondary,
  }),
  note: css({
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
    marginTop: theme.spacing(1),
  }),
});
