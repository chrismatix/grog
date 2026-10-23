import { css, cx } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { Icon, Tooltip, useStyles2 } from "@grafana/ui";

export interface Comparison {
  previous: number;
  // Rates compare in percentage points, everything else as a relative change.
  kind: "relative" | "points";
  // Unset for volumes like build counts, which are neither good nor bad.
  higherIsBetter?: boolean;
}

export interface Stat {
  label: string;
  value: string;
  current?: number;
  comparison?: Comparison;
  hint?: string;
}

function change(current: number, comparison: Comparison) {
  if (!Number.isFinite(current) || !Number.isFinite(comparison.previous)) {
    return undefined;
  }
  const amount =
    comparison.kind === "points"
      ? (current - comparison.previous) * 100
      : comparison.previous === 0
        ? Number.NaN
        : ((current - comparison.previous) / comparison.previous) * 100;
  if (!Number.isFinite(amount)) {
    return undefined;
  }
  const unit = comparison.kind === "points" ? " pp" : "%";
  const text = `${amount > 0 ? "+" : ""}${Math.abs(amount) < 10 ? amount.toFixed(1) : Math.round(amount)}${unit}`;
  const negligible = Math.abs(amount) < (comparison.kind === "points" ? 0.5 : 2);
  const better = amount > 0 === comparison.higherIsBetter;
  const tone: "neutral" | "good" | "bad" =
    negligible || comparison.higherIsBetter === undefined ? "neutral" : better ? "good" : "bad";
  return { amount, text, tone };
}

export function StatGrid({ stats, previousLabel }: { stats: Stat[]; previousLabel?: string }) {
  const styles = useStyles2(getStyles);
  return (
    <dl className={styles.grid}>
      {stats.map((stat) => {
        const delta =
          stat.comparison && stat.current !== undefined
            ? change(stat.current, stat.comparison)
            : undefined;
        return (
          <div key={stat.label} className={styles.stat}>
            <dt>
              {stat.label}
              {stat.hint && (
                <Tooltip content={stat.hint}>
                  <Icon name="info-circle" size="xs" className={styles.hint} />
                </Tooltip>
              )}
            </dt>
            <dd>{stat.value}</dd>
            {delta && (
              <dd
                className={cx(styles.delta, styles[delta.tone])}
                title={previousLabel ? `vs ${previousLabel}` : undefined}
              >
                <Icon name={delta.amount > 0 ? "arrow-up" : "arrow-down"} size="xs" /> {delta.text}
              </dd>
            )}
          </div>
        );
      })}
      {previousLabel && <p className={styles.versus}>Changes are against the {previousLabel}.</p>}
    </dl>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  grid: css({
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(170px, 1fr))",
    gap: theme.spacing(1.5),
    margin: theme.spacing(0, 0, 2),
  }),
  stat: css({
    background: theme.colors.background.primary,
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    padding: theme.spacing(1.5, 2),
    dt: {
      color: theme.colors.text.secondary,
      fontSize: theme.typography.bodySmall.fontSize,
      display: "flex",
      alignItems: "center",
      gap: theme.spacing(0.5),
    },
    "dd:nth-of-type(1)": {
      margin: theme.spacing(0.5, 0, 0),
      fontSize: theme.typography.h3.fontSize,
      fontVariantNumeric: "tabular-nums",
    },
  }),
  hint: css({ color: theme.colors.text.disabled }),
  delta: css({
    margin: 0,
    fontSize: theme.typography.bodySmall.fontSize,
    fontVariantNumeric: "tabular-nums",
  }),
  versus: css({
    gridColumn: "1 / -1",
    margin: 0,
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
  }),
  good: css({ color: theme.colors.success.text }),
  bad: css({ color: theme.colors.error.text }),
  neutral: css({ color: theme.colors.text.secondary }),
});
