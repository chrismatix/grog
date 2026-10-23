import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { Icon, TextLink, Tooltip, useStyles2 } from "@grafana/ui";
import type { ReactNode } from "react";

import { type SqlState, SqlStatus } from "../useSql";

export function Panel({
  title,
  help,
  actions,
  viewAll,
  state,
  empty,
  children,
}: {
  title: string;
  help?: string;
  actions?: ReactNode;
  viewAll?: string;
  state?: SqlState;
  // Shown instead of the children once loaded with no rows.
  empty?: ReactNode;
  children?: ReactNode;
}) {
  const styles = useStyles2(getStyles);
  const ready = !state || (!state.loading && !state.error);
  return (
    <section className={styles.panel}>
      <header className={styles.header}>
        <h4 className={styles.title}>
          {title}
          {help && (
            <Tooltip content={help}>
              <Icon name="info-circle" size="sm" className={styles.help} />
            </Tooltip>
          )}
        </h4>
        <div className={styles.actions}>
          {actions}
          {viewAll && (
            <TextLink href={viewAll} inline={false} icon="arrow-right">
              View all
            </TextLink>
          )}
        </div>
      </header>
      {state && <SqlStatus state={state} />}
      {ready &&
        (state && state.rows.length === 0 && empty ? (
          <p className={styles.empty}>{empty}</p>
        ) : (
          children
        ))}
    </section>
  );
}

export function PanelGrid({
  minimumWidth = 480,
  children,
}: {
  minimumWidth?: number;
  children: ReactNode;
}) {
  const styles = useStyles2(getStyles);
  return (
    <div
      className={styles.grid}
      style={{
        gridTemplateColumns: `repeat(auto-fit, minmax(min(${minimumWidth}px, 100%), 1fr))`,
      }}
    >
      {children}
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  panel: css({
    background: theme.colors.background.primary,
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    padding: theme.spacing(2),
    marginBottom: theme.spacing(2),
    minWidth: 0,
    overflowX: "auto",
  }),
  header: css({
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "space-between",
    gap: theme.spacing(1),
    marginBottom: theme.spacing(1.5),
  }),
  title: css({
    margin: 0,
    display: "flex",
    alignItems: "center",
    gap: theme.spacing(0.75),
  }),
  help: css({ color: theme.colors.text.secondary }),
  actions: css({
    display: "flex",
    alignItems: "center",
    gap: theme.spacing(2),
  }),
  empty: css({ color: theme.colors.text.secondary, margin: 0 }),
  grid: css({
    display: "grid",
    gap: theme.spacing(2),
    "& > section": { marginBottom: 0 },
    marginBottom: theme.spacing(2),
  }),
});
