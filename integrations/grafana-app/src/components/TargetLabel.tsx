import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { Badge, TextLink, useStyles2 } from "@grafana/ui";

import { targetUrl } from "../links";

export function parseTags(json: unknown): string[] {
  try {
    const tags = JSON.parse(String(json || "[]"));
    return Array.isArray(tags) ? tags.map(String) : [];
  } catch {
    return [];
  }
}

export function TargetTags({ tags }: { tags: string[] }) {
  return (
    <>
      {tags.map((tag) => (
        <Badge key={tag} text={tag} color={tag === "no-cache" ? "orange" : "blue"} />
      ))}
    </>
  );
}

export function TargetLabel({ label, tags = [] }: { label: string; tags?: string[] }) {
  const styles = useStyles2(getStyles);
  return (
    <span className={styles.label}>
      <TextLink href={targetUrl(label)} inline={false} title={label}>
        {label}
      </TextLink>
      <TargetTags tags={tags} />
    </span>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  label: css({
    display: "inline-flex",
    alignItems: "center",
    gap: theme.spacing(0.5),
    maxWidth: 520,
    fontFamily: theme.typography.fontFamilyMonospace,
    "& > a": {
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap",
    },
  }),
});
