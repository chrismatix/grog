import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { TextLink, useStyles2, useTheme2 } from "@grafana/ui";

import { formatDuration, targetUrl } from "../links";
import { phases } from "../phases";
import type { TargetSpan } from "./DependencyGraph";

export function Timeline({
  spans,
  buildStartMs,
  buildDurationMs,
}: {
  spans: TargetSpan[];
  buildStartMs: number;
  buildDurationMs: number;
}) {
  const styles = useStyles2(getStyles);
  const theme = useTheme2();
  const colorOf = (index: number) => theme.visualization.getColorByName(phases[index].color);
  const rows = [...spans].sort((left, right) => left.startMs - right.startMs);
  const total = Math.max(buildDurationMs, 1);

  return (
    <div>
      <div className={styles.legend}>
        {phases.map((phase, index) => (
          <span key={phase.key}>
            <span className={styles.swatch} style={{ background: colorOf(index) }} /> {phase.label}
          </span>
        ))}
      </div>
      {rows.map((span) => {
        const left = ((span.startMs - buildStartMs) / total) * 100;
        const width = Math.max((span.durationMs / total) * 100, 0.2);
        return (
          <div key={span.label} className={styles.row}>
            <div className={styles.label} title={span.label}>
              <TextLink href={targetUrl(span.label)} inline={false}>
                {span.label}
              </TextLink>
            </div>
            <div className={styles.track}>
              <div
                className={styles.bar}
                style={{ left: `${Math.max(left, 0)}%`, width: `${width}%` }}
                title={phases
                  .map((phase) => `${phase.label}: ${formatDuration(span.phases[phase.key] ?? 0)}`)
                  .join("\n")}
              >
                {phases.map((phase, index) => (
                  <div
                    key={phase.key}
                    style={{
                      flexGrow: span.phases[phase.key] ?? 0,
                      background: colorOf(index),
                    }}
                  />
                ))}
              </div>
            </div>
            <div className={styles.duration}>{formatDuration(span.durationMs)}</div>
          </div>
        );
      })}
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  legend: css({
    display: "flex",
    flexWrap: "wrap",
    gap: theme.spacing(1.5),
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
    marginBottom: theme.spacing(1),
  }),
  swatch: css({
    display: "inline-block",
    width: 10,
    height: 10,
    borderRadius: 2,
  }),
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
    fontFamily: theme.typography.fontFamilyMonospace,
  }),
  track: css({
    position: "relative",
    height: 12,
    background: theme.colors.background.secondary,
  }),
  bar: css({
    position: "absolute",
    top: 0,
    bottom: 0,
    display: "flex",
    minWidth: 2,
    overflow: "hidden",
  }),
  duration: css({
    textAlign: "right",
    fontVariantNumeric: "tabular-nums",
    color: theme.colors.text.secondary,
  }),
});
