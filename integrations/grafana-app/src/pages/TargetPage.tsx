import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { PluginPage } from "@grafana/runtime";
import { TextLink, useStyles2 } from "@grafana/ui";
import { useMemo } from "react";

import { FilterBar } from "../components/AppPage";
import { DataTable } from "../components/DataTable";
import { Panel, PanelGrid } from "../components/Panel";
import { RunScatter } from "../components/RunScatter";
import { StatGrid } from "../components/StatGrid";
import { parseTags, TargetTags } from "../components/TargetLabel";
import { type Filters, rangeLabel } from "../filters";
import {
  appUrl,
  buildUrl,
  formatCount,
  formatDuration,
  formatPercent,
  formatTime,
  targetUrl,
} from "../links";
import { dependentsQuery } from "../sql/builds";
import { targetRunsQuery } from "../sql/targets";
import { getTableStyles } from "../styles";
import { SqlStatus, useSql } from "../useSql";

interface Run {
  traceId: string;
  isCurrent: boolean;
  onCriticalPath: boolean;
  tags: string[];
  startMs: number;
  commandMs: number;
  executed: boolean;
  failed: boolean;
  changeHash: string;
  arch: string;
  dependencies: string[];
  buildStartMs: number;
  buildDurationMs: number;
  job: string;
  branch: string;
}

function quantile(values: number[], fraction: number): number {
  if (values.length === 0) {
    return 0;
  }
  const sorted = [...values].sort((left, right) => left - right);
  return sorted[Math.min(sorted.length - 1, Math.floor(fraction * sorted.length))];
}

export function TargetPage({ label, filters }: { label: string; filters: Filters }) {
  const styles = useStyles2(getTableStyles);
  const pageStyles = useStyles2(getStyles);
  const runRows = useSql(targetRunsQuery(label, filters));

  const allRuns: Run[] = useMemo(
    () =>
      runRows.rows.map((row) => ({
        traceId: String(row.trace_id),
        isCurrent: Boolean(Number(row.is_current)),
        onCriticalPath: Boolean(Number(row.on_critical_path)),
        tags: parseTags(row.tags),
        startMs: Number(row.start_ms),
        commandMs: Number(row.command_ms),
        executed: Boolean(Number(row.executed)),
        failed: Boolean(Number(row.failed)),
        changeHash: String(row.change_hash),
        arch: String(row.arch),
        dependencies: JSON.parse(String(row.dependencies || "[]")),
        buildStartMs: Number(row.build_start_ms),
        buildDurationMs: Number(row.build_duration_ms),
        job: String(row.job ?? ""),
        branch: String(row.branch ?? ""),
      })),
    [runRows.rows],
  );
  const runs = useMemo(() => allRuns.filter((run) => run.isCurrent), [allRuns]);
  const previousExecuted = allRuns.filter((run) => !run.isCurrent && run.executed);
  const executedRuns = runs.filter((run) => run.executed);
  const recentRuns = useMemo(() => runs.slice(-50).reverse(), [runs]);
  const latest = runs.at(-1);
  const dependents = useSql(
    latest?.buildStartMs
      ? dependentsQuery(label, latest.traceId, latest.buildStartMs, latest.buildDurationMs)
      : undefined,
  );

  const hashes = useMemo(() => {
    const byHash = new Map<string, Run[]>();
    runs.forEach((run) => byHash.set(run.changeHash, [...(byHash.get(run.changeHash) ?? []), run]));
    return [...byHash.entries()]
      .map(([hash, hashRuns]) => ({
        hash,
        firstSeen: hashRuns[0].startMs,
        runs: hashRuns.length,
        executed: hashRuns.filter((run) => run.executed).length,
        failures: hashRuns.filter((run) => run.failed).length,
        medianCommandMs: quantile(
          hashRuns.filter((run) => run.executed).map((run) => run.commandMs),
          0.5,
        ),
      }))
      .reverse();
  }, [runs]);
  const flakyHashes = hashes.filter(
    (hash) => hash.failures > 0 && hash.failures < hash.runs,
  ).length;

  const median = quantile(
    executedRuns.map((run) => run.commandMs),
    0.5,
  );
  const previousMedian = quantile(
    previousExecuted.map((run) => run.commandMs),
    0.5,
  );
  const hitRate = runs.length ? (runs.length - executedRuns.length) / runs.length : Number.NaN;
  const previousRuns = allRuns.length - runs.length;
  const previousHitRate = previousRuns
    ? (previousRuns - previousExecuted.length) / previousRuns
    : Number.NaN;
  const tags = latest?.tags ?? [];

  return (
    <PluginPage
      pageNav={{
        text: label,
        parentItem: { text: "Targets", url: appUrl("targets") },
      }}
    >
      <FilterBar filters={filters} />
      <SqlStatus state={runRows} />
      {!runRows.loading && !runRows.error && (
        <>
          {tags.length > 0 && (
            <p className={pageStyles.tags}>
              Tags: <TargetTags tags={tags} />
            </p>
          )}
          <StatGrid
            previousLabel={`previous ${rangeLabel(filters.range)}`}
            stats={[
              { label: "Runs", value: formatCount(runs.length) },
              {
                label: "Cache hit rate",
                value: formatPercent(hitRate),
                current: hitRate,
                comparison: {
                  previous: previousHitRate,
                  kind: "points",
                  higherIsBetter: true,
                },
              },
              {
                label: "Median command",
                value: formatDuration(executedRuns.length ? median : Number.NaN),
                current: median,
                comparison: previousExecuted.length
                  ? {
                      previous: previousMedian,
                      kind: "relative",
                      higherIsBetter: false,
                    }
                  : undefined,
              },
              {
                label: "p95 command",
                value: formatDuration(
                  executedRuns.length
                    ? quantile(
                        executedRuns.map((run) => run.commandMs),
                        0.95,
                      )
                    : Number.NaN,
                ),
              },
              {
                label: "Time executing",
                value: formatDuration(executedRuns.reduce((sum, run) => sum + run.commandMs, 0)),
              },
              {
                label: "On critical path",
                value: formatCount(runs.filter((run) => run.onCriticalPath).length),
                hint: "Builds in which this target was on the critical path (grog v0.49.0+)",
              },
              {
                label: "Failures",
                value: formatCount(runs.filter((run) => run.failed).length),
              },
              {
                label: "Flaky change hashes",
                value: formatCount(flakyHashes),
                hint: "Change hashes that both failed and passed",
              },
            ]}
          />

          <Panel
            title="Command time per executed run"
            help="Click a point to open its build. The line is the daily median."
          >
            <RunScatter runs={executedRuns.filter((run) => run.buildStartMs > 0)} />
          </Panel>

          <PanelGrid minimumWidth={320}>
            <Panel title="Dependencies">
              <LabelList labels={latest?.dependencies ?? []} />
            </Panel>
            <Panel title="Dependents" state={dependents}>
              <LabelList labels={dependents.rows.map((row) => String(row.label))} />
            </Panel>
          </PanelGrid>

          <Panel
            title="Change hashes"
            help="One row per distinct set of inputs. A hash that both failed and passed is flaky; one executed more than once missed the cache."
          >
            <DataTable
              rows={hashes}
              rowKey={(hash) => hash.hash}
              sortedBy="First seen"
              columns={[
                {
                  label: "Change hash",
                  className: () => pageStyles.monospace,
                  render: (hash) => (
                    <span title={hash.hash}>
                      {hash.hash
                        .split("_")
                        .map((part) => part.slice(0, 8))
                        .join("_")}
                    </span>
                  ),
                },
                {
                  label: "First seen",
                  sortValue: (hash) => hash.firstSeen,
                  render: (hash) => formatTime(hash.firstSeen),
                },
                {
                  label: "Runs",
                  numeric: true,
                  sortValue: (hash) => hash.runs,
                  render: (hash) => hash.runs,
                },
                {
                  label: "Executed",
                  numeric: true,
                  sortValue: (hash) => hash.executed,
                  className: (hash) => (hash.executed > 1 ? styles.warning : ""),
                  render: (hash) => hash.executed,
                },
                {
                  label: "Failures",
                  numeric: true,
                  sortValue: (hash) => hash.failures,
                  className: (hash) => (hash.failures > 0 ? styles.error : ""),
                  render: (hash) =>
                    `${hash.failures}${hash.failures > 0 && hash.failures < hash.runs ? " · flaky" : ""}`,
                },
                {
                  label: "Median command",
                  numeric: true,
                  sortValue: (hash) => (hash.executed ? hash.medianCommandMs : Number.NaN),
                  render: (hash) => (hash.executed ? formatDuration(hash.medianCommandMs) : "—"),
                },
              ]}
            />
          </Panel>

          <Panel title="Recent runs">
            <DataTable
              rows={recentRuns}
              rowKey={(run) => `${run.traceId}-${run.startMs}`}
              sortedBy="Build"
              columns={[
                {
                  label: "Build",
                  sortValue: (run) => run.startMs,
                  render: (run) =>
                    run.buildStartMs ? (
                      <TextLink
                        href={buildUrl(run.traceId, run.buildStartMs, run.buildDurationMs)}
                        inline={false}
                      >
                        {formatTime(run.startMs)}
                      </TextLink>
                    ) : (
                      formatTime(run.startMs)
                    ),
                },
                { label: "Job", render: (run) => run.job },
                {
                  label: "Branch",
                  className: () => styles.truncate,
                  render: (run) => <span title={run.branch}>{run.branch}</span>,
                },
                {
                  label: "Arch",
                  className: () => styles.muted,
                  render: (run) => run.arch,
                },
                {
                  label: "Result",
                  className: (run) => (run.failed ? styles.error : ""),
                  render: (run) =>
                    run.failed ? "failed" : run.executed ? "executed" : "cache hit",
                },
                {
                  label: "Critical path",
                  render: (run) => (run.onCriticalPath ? "yes" : ""),
                },
                {
                  label: "Command",
                  numeric: true,
                  sortValue: (run) => (run.executed ? run.commandMs : Number.NaN),
                  render: (run) => (run.executed ? formatDuration(run.commandMs) : "—"),
                },
              ]}
            />
          </Panel>
        </>
      )}
    </PluginPage>
  );
}

function LabelList({ labels }: { labels: string[] }) {
  const styles = useStyles2(getStyles);
  if (labels.length === 0) {
    return <p className={styles.empty}>None</p>;
  }
  return (
    <ul className={styles.list}>
      {labels.map((label) => (
        <li key={label}>
          <TextLink href={targetUrl(label)} inline={false}>
            {label}
          </TextLink>
        </li>
      ))}
    </ul>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  tags: css({
    display: "flex",
    alignItems: "center",
    gap: theme.spacing(0.5),
    color: theme.colors.text.secondary,
  }),
  list: css({
    listStyle: "none",
    padding: 0,
    margin: 0,
    fontFamily: theme.typography.fontFamilyMonospace,
    fontSize: theme.typography.bodySmall.fontSize,
    li: { padding: theme.spacing(0.25, 0) },
  }),
  empty: css({ color: theme.colors.text.secondary }),
  monospace: css({ fontFamily: theme.typography.fontFamilyMonospace }),
});
