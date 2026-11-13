import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { PluginPage } from "@grafana/runtime";
import { RadioButtonGroup, TextLink, useStyles2 } from "@grafana/ui";
import { useMemo, useState } from "react";

import { DataTable } from "../components/DataTable";
import { Panel, PanelGrid } from "../components/Panel";
import type { TargetSpan } from "../components/DependencyGraph";
import { type Stat, StatGrid } from "../components/StatGrid";
import { TargetLabel } from "../components/TargetLabel";
import { appUrl, type BuildReference, buildUrl, formatDuration, formatTime } from "../links";
import { type Build, branchOf, isExecuted, useBuild } from "../useBuild";
import { SqlStatus } from "../useSql";

interface TargetDiff {
  label: string;
  base?: TargetSpan;
  head?: TargetSpan;
  baseMs: number;
  headMs: number;
}

function resultOf(span: TargetSpan | undefined): string {
  if (!span) {
    return "absent";
  }
  if (span.failed) {
    return "failed";
  }
  return isExecuted(span) ? "executed" : "cache hit";
}

const timeOf = (span: TargetSpan | undefined) => (span && isExecuted(span) ? span.commandMs : 0);

// A target "changed" when it ran in only one build, failed in only one, or its command time moved by a second.
const significantMs = 1000;

function isChanged(diff: TargetDiff): boolean {
  const ran = (span: TargetSpan | undefined) => !!span && isExecuted(span);
  return (
    ran(diff.base) !== ran(diff.head) ||
    !!diff.base?.failed !== !!diff.head?.failed ||
    Math.abs(diff.headMs - diff.baseMs) >= significantMs
  );
}

function summaryStats(
  head: Build,
  base: Build,
  headRef: BuildReference,
  baseRef: BuildReference,
): Stat[] {
  const metric = (
    label: string,
    headValue: number,
    baseValue: number,
    format: (value: number) => string,
  ): Stat => ({
    label,
    value: format(headValue),
    current: headValue,
    comparison: {
      previous: baseValue,
      kind: "relative",
      higherIsBetter: false,
    },
  });
  const count = (value: number) => String(value);
  return [
    metric("Duration", headRef.durationMs, baseRef.durationMs, formatDuration),
    metric(
      "Critical path",
      Number(head.attributes["grog.critical_path_exec_ms"] ?? Number.NaN),
      Number(base.attributes["grog.critical_path_exec_ms"] ?? Number.NaN),
      formatDuration,
    ),
    metric(
      "Executed targets",
      head.spans.filter(isExecuted).length,
      base.spans.filter(isExecuted).length,
      count,
    ),
    metric(
      "Command time",
      head.spans.reduce((sum, span) => sum + timeOf(span), 0),
      base.spans.reduce((sum, span) => sum + timeOf(span), 0),
      formatDuration,
    ),
    metric(
      "Failures",
      Number(head.attributes["grog.failure_count"] ?? 0),
      Number(base.attributes["grog.failure_count"] ?? 0),
      count,
    ),
  ];
}

export function ComparePage({ base, head }: { base: BuildReference; head: BuildReference }) {
  const styles = useStyles2(getStyles);
  const [changedOnly, setChangedOnly] = useState(true);
  const baseBuild = useBuild(base);
  const headBuild = useBuild(head);
  const loading = baseBuild.loading || headBuild.loading;
  const error = baseBuild.error ?? headBuild.error;

  const diffs: TargetDiff[] = useMemo(() => {
    const baseByLabel = new Map(baseBuild.spans.map((span) => [span.label, span]));
    const headByLabel = new Map(headBuild.spans.map((span) => [span.label, span]));
    const labels = new Set([...baseByLabel.keys(), ...headByLabel.keys()]);
    return [...labels].map((label) => ({
      label,
      base: baseByLabel.get(label),
      head: headByLabel.get(label),
      baseMs: timeOf(baseByLabel.get(label)),
      headMs: timeOf(headByLabel.get(label)),
    }));
  }, [baseBuild.spans, headBuild.spans]);
  const changed = diffs.filter(isChanged);

  const describe = (build: Build, reference: BuildReference, role: string) => (
    <div className={styles.side}>
      <span className={styles.role}>{role}</span>
      <TextLink
        href={buildUrl(reference.traceId, reference.startMs, reference.durationMs)}
        inline={false}
      >
        {formatTime(reference.startMs)}
      </TextLink>
      <span className={styles.muted}>
        {build.attributes["grog.tag.github.workflow"]
          ? `${build.attributes["grog.tag.github.workflow"]} / ${build.attributes["grog.tag.github.job"]}`
          : build.command}{" "}
        · {branchOf(build.attributes)} ·{" "}
        {String(build.attributes["grog.git_commit"] ?? "").slice(0, 10)}
      </span>
    </div>
  );

  return (
    <PluginPage
      pageNav={{
        text: "Compare builds",
        parentItem: { text: "Builds", url: appUrl("builds") },
      }}
    >
      <SqlStatus state={{ rows: [], loading, error }} />
      {!loading && !error && (
        <>
          <PanelGrid minimumWidth={360}>
            {describe(baseBuild, base, "Base")}
            {describe(headBuild, head, "Compared")}
          </PanelGrid>
          <StatGrid previousLabel="base" stats={summaryStats(headBuild, baseBuild, head, base)} />

          <Panel
            title={`Targets · ${changed.length} changed`}
            help={`Command time of executed runs; a cache hit counts as zero. Changed means it executed or failed in only one build, or its time differs by at least ${formatDuration(significantMs)}.`}
            actions={
              <RadioButtonGroup
                size="sm"
                value={changedOnly}
                options={[
                  { label: "Changed", value: true },
                  { label: `All ${diffs.length}`, value: false },
                ]}
                onChange={setChangedOnly}
              />
            }
          >
            <DataTable
              key={String(changedOnly)}
              rows={changedOnly ? changed : diffs}
              rowKey={(diff) => diff.label}
              sortedBy="Difference"
              columns={[
                {
                  label: "Target",
                  render: (diff) => <TargetLabel label={diff.label} />,
                },
                {
                  label: "Base",
                  render: (diff) => (
                    <span className={styles[resultClass(resultOf(diff.base))]}>
                      {resultOf(diff.base)}
                    </span>
                  ),
                },
                {
                  label: "Compared",
                  render: (diff) => (
                    <span className={styles[resultClass(resultOf(diff.head))]}>
                      {resultOf(diff.head)}
                    </span>
                  ),
                },
                {
                  label: "Base time",
                  numeric: true,
                  sortValue: (diff) => diff.baseMs,
                  render: (diff) => formatDuration(diff.baseMs),
                },
                {
                  label: "Compared time",
                  numeric: true,
                  sortValue: (diff) => diff.headMs,
                  render: (diff) => formatDuration(diff.headMs),
                },
                {
                  label: "Difference",
                  numeric: true,
                  sortValue: (diff) => Math.abs(diff.headMs - diff.baseMs),
                  className: (diff) =>
                    diff.headMs - diff.baseMs >= significantMs
                      ? styles.bad
                      : diff.baseMs - diff.headMs >= significantMs
                        ? styles.good
                        : "",
                  render: (diff) => {
                    const difference = diff.headMs - diff.baseMs;
                    return `${difference > 0 ? "+" : ""}${formatDuration(difference)}`;
                  },
                },
                {
                  label: "Critical path",
                  render: (diff) =>
                    [
                      baseBuild.criticalPath.includes(diff.label) && "base",
                      headBuild.criticalPath.includes(diff.label) && "compared",
                    ]
                      .filter(Boolean)
                      .join(" · "),
                },
              ]}
            />
          </Panel>
        </>
      )}
    </PluginPage>
  );
}

function resultClass(result: string): "bad" | "muted" | "plain" {
  if (result === "failed") {
    return "bad";
  }
  return result === "cache hit" || result === "absent" ? "muted" : "plain";
}

const getStyles = (theme: GrafanaTheme2) => ({
  side: css({
    display: "flex",
    flexDirection: "column",
    gap: theme.spacing(0.5),
    padding: theme.spacing(1.5, 2),
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    background: theme.colors.background.primary,
  }),
  role: css({
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
    textTransform: "uppercase",
    letterSpacing: "0.05em",
  }),
  muted: css({ color: theme.colors.text.secondary }),
  plain: css({}),
  bad: css({ color: theme.colors.error.text }),
  good: css({ color: theme.colors.success.text }),
});
