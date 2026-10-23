import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";

export const getTableStyles = (theme: GrafanaTheme2) => ({
  table: css({
    width: "100%",
    borderCollapse: "collapse",
    fontSize: theme.typography.bodySmall.fontSize,
    "th, td": {
      padding: theme.spacing(0.75, 1),
      borderBottom: `1px solid ${theme.colors.border.weak}`,
      textAlign: "left",
      whiteSpace: "nowrap",
    },
    th: {
      color: theme.colors.text.secondary,
      fontWeight: theme.typography.fontWeightMedium,
    },
    "td.number, th.number": {
      textAlign: "right",
      fontVariantNumeric: "tabular-nums",
    },
    "tbody tr:hover": { background: theme.colors.action.hover },
  }),
  sortable: css({
    cursor: "pointer",
    userSelect: "none",
    "&:hover": { color: theme.colors.text.primary },
  }),
  muted: css({ color: theme.colors.text.secondary }),
  truncate: css({
    maxWidth: 360,
    overflow: "hidden",
    textOverflow: "ellipsis",
  }),
  error: css({ color: theme.colors.error.text }),
  warning: css({ color: theme.colors.warning.text }),
  section: css({ marginBottom: theme.spacing(3) }),
  toolbar: css({
    display: "flex",
    flexWrap: "wrap",
    gap: theme.spacing(1),
    alignItems: "flex-end",
    marginBottom: theme.spacing(2),
  }),
});
