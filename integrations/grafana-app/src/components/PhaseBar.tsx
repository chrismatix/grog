import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { useStyles2, useTheme2 } from "@grafana/ui";

import { formatDuration, formatPercent } from "../links";
import { phases } from "../phases";

export function PhaseBar({ totals }: { totals: Record<string, number> }) {
  const styles = useStyles2(getStyles);
  const theme = useTheme2();
  const sum = phases.reduce((total, phase) => total + (totals[phase.key] ?? 0), 0);
  if (sum === 0) {
    return null;
  }
  return (
    <div>
      <div className={styles.bar}>
        {phases.map((phase) => (
          <div
            key={phase.key}
            title={`${phase.label}: ${formatDuration(totals[phase.key] ?? 0)}`}
            style={{
              flexGrow: totals[phase.key] ?? 0,
              background: theme.visualization.getColorByName(phase.color),
            }}
          />
        ))}
      </div>
      <ul className={styles.legend}>
        {phases.map((phase) => (
          <li key={phase.key}>
            <span
              className={styles.swatch}
              style={{
                background: theme.visualization.getColorByName(phase.color),
              }}
            />
            <span>{phase.label}</span>
            <span className={styles.value}>
              {formatPercent((totals[phase.key] ?? 0) / sum)} ·{" "}
              {formatDuration(totals[phase.key] ?? 0)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  bar: css({
    display: "flex",
    height: 20,
    borderRadius: theme.shape.radius.default,
    overflow: "hidden",
    marginBottom: theme.spacing(1.5),
    "& > div": { minWidth: 1 },
  }),
  legend: css({
    listStyle: "none",
    margin: 0,
    padding: 0,
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))",
    gap: theme.spacing(0.5, 2),
    fontSize: theme.typography.bodySmall.fontSize,
    li: { display: "flex", alignItems: "center", gap: theme.spacing(0.75) },
  }),
  swatch: css({
    width: 10,
    height: 10,
    borderRadius: 2,
    flexShrink: 0,
  }),
  value: css({
    marginLeft: "auto",
    color: theme.colors.text.secondary,
    fontVariantNumeric: "tabular-nums",
  }),
});
