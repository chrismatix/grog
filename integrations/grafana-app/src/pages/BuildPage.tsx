import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { PluginPage } from "@grafana/runtime";
import { Alert, LinkButton, TextLink, useStyles2 } from "@grafana/ui";
import { useMemo } from "react";

import { DataTable } from "../components/DataTable";
import { DependencyGraph } from "../components/DependencyGraph";
import { JobSteps } from "../components/JobSteps";
import { Panel, PanelGrid } from "../components/Panel";
import { PhaseBar } from "../components/PhaseBar";
import { StatGrid } from "../components/StatGrid";
import { TargetLabel } from "../components/TargetLabel";
import { Timeline } from "../components/Timeline";
import {
  appUrl,
  type BuildReference,
  compareUrl,
  formatCount,
  formatDuration,
  formatTime,
  githubRunUrl,
} from "../links";
import { phases } from "../phases";
import { previousBuildQuery } from "../sql/builds";
import { buildJobQuery } from "../sql/jobs";
import { branchOf, criticalPathSavings, isExecuted, useBuild } from "../useBuild";
import { SqlStatus, useSql } from "../useSql";

export function BuildPage({ build }: { build: BuildReference }) {
  const styles = useStyles2(getStyles);
  const { loading, error, command, arch, attributes, criticalPath, spans } = useBuild(build);
  const workflow = attributes["grog.tag.github.workflow"] ?? "";
  const job = attributes["grog.tag.github.job"] ?? "";
  const branch = branchOf(attributes);
  const runId = attributes["grog.tag.github.run_id"];
  const runAttempt = attributes["grog.tag.github.run_attempt"] ?? "1";

  const previous = useSql(
    workflow && job && branch
      ? previousBuildQuery(build.startMs, workflow, job, branch)
      : undefined,
  );
  const jobSpans = useSql(runId ? buildJobQuery(runId, runAttempt, job, build.startMs) : undefined);

  const executed = spans.filter((span) => isExecuted(span) || criticalPath.includes(span.label));
  const hasEdges = spans.some((span) => span.dependencies.length > 0);
  const savings = useMemo(
    () => (hasEdges ? criticalPathSavings(spans, criticalPath) : []),
    [hasEdges, spans, criticalPath],
  );
  const commandTotal = spans.reduce((sum, span) => sum + span.commandMs, 0);
  const phaseTotals = Object.fromEntries(
    phases.map((phase) => [
      phase.key,
      spans.reduce((sum, span) => sum + (span.phases[phase.key] ?? 0), 0),
    ]),
  );
  const previousBuild = previous.rows[0];

  return (
    <PluginPage
      pageNav={{
        text: `${command} · ${formatTime(build.startMs)}`,
        parentItem: { text: "Builds", url: appUrl("builds") },
      }}
    >
      <SqlStatus state={{ rows: [], loading, error }} />
      {!loading && !error && (
        <>
          <div className={styles.header}>
            <div className={styles.context}>
              <span>{workflow ? `${workflow} / ${job}` : "No GitHub context"}</span>
              {branch && <span className={styles.muted}>{branch}</span>}
              {arch && <span className={styles.muted}>{arch}</span>}
              <span className={styles.muted}>
                {String(attributes["grog.git_commit"] ?? "").slice(0, 10)}
              </span>
              {runId && (
                <TextLink
                  href={githubRunUrl(attributes["grog.tag.github.repository"], runId, runAttempt)}
                  external
                >
                  {`GitHub run #${runId}${Number(runAttempt) > 1 ? ` · attempt ${runAttempt}` : ""}`}
                </TextLink>
              )}
            </div>
            <LinkButton
              size="sm"
              variant="secondary"
              icon="exchange-alt"
              disabled={!previousBuild}
              tooltip={
                previousBuild
                  ? `Previous ${workflow} / ${job} build on ${branch}, ${formatTime(Number(previousBuild.start_ms))}`
                  : "No earlier build of this job on this branch in the last 30 days"
              }
              href={
                previousBuild
                  ? compareUrl(
                      {
                        traceId: String(previousBuild.trace_id),
                        startMs: Number(previousBuild.start_ms),
                        durationMs: Number(previousBuild.duration_ms),
                      },
                      build,
                    )
                  : undefined
              }
            >
              Compare with previous build
            </LinkButton>
          </div>

          <StatGrid
            stats={[
              { label: "Duration", value: formatDuration(build.durationMs) },
              {
                label: "Critical path",
                value: formatDuration(
                  Number(attributes["grog.critical_path_exec_ms"] ?? Number.NaN),
                ),
                hint: "Execution time along the longest dependency chain",
              },
              {
                label: "Executed / targets",
                value: `${formatCount(spans.filter(isExecuted).length)} / ${formatCount(spans.length)}`,
              },
              {
                label: "Average parallelism",
                value: (commandTotal / Math.max(build.durationMs, 1)).toFixed(1),
                hint: "Summed command time divided by build duration: how many targets ran at once on average",
              },
              {
                label: "Queue wait",
                value: formatDuration(phaseTotals.queue_wait_ms),
                hint: "Summed time ready targets waited for a free worker. High values mean the build is worker-bound, not dependency-bound.",
              },
              {
                label: "Async cache wait",
                value: formatDuration(Number(attributes["grog.async_cache_wait_ms"] ?? Number.NaN)),
                hint: "Time at the end waiting for cache uploads",
              },
              {
                label: "Failures",
                value: formatCount(Number(attributes["grog.failure_count"] ?? 0)),
              },
            ]}
          />

          {!hasEdges && spans.length > 0 && (
            <Alert severity="info" title="">
              This build predates grog v0.49.0, so its trace has no dependency edges or critical
              path.
            </Alert>
          )}

          <PanelGrid minimumWidth={520}>
            {savings.length > 0 && (
              <Panel
                title="What if a critical-path target were instant?"
                help="Recomputes the longest dependency chain with that target taking no time. An estimate: it assumes enough free workers."
              >
                <DataTable
                  rows={savings}
                  rowKey={(saving) => saving.label}
                  sortedBy="Build saves"
                  columns={[
                    {
                      label: "Target",
                      render: (saving) => <TargetLabel label={saving.label} />,
                    },
                    {
                      label: "Its time",
                      numeric: true,
                      sortValue: (saving) => saving.occupiedMs,
                      render: (saving) => formatDuration(saving.occupiedMs),
                    },
                    {
                      label: "Build saves",
                      numeric: true,
                      sortValue: (saving) => saving.savingMs,
                      render: (saving) => formatDuration(saving.savingMs),
                    },
                  ]}
                />
              </Panel>
            )}
            <Panel title="Where target time went" help="Summed over every target in this build">
              <PhaseBar totals={phaseTotals} />
            </Panel>
          </PanelGrid>

          <Panel title="Dependency graph">
            <DependencyGraph spans={spans} criticalPath={criticalPath} />
          </Panel>

          <Panel title="Timeline · executed and critical-path targets">
            <Timeline
              spans={executed}
              buildStartMs={build.startMs}
              buildDurationMs={build.durationMs}
            />
          </Panel>

          {runId && (
            <Panel
              title="GitHub job"
              help="The whole Actions job around this build: runner queue, setup steps and everything after grog"
              state={jobSpans}
              empty="No job timings for this run. They are exported by the github-job-spans.sh example, in workflows with the actions: read permission."
            >
              <JobSteps
                rows={jobSpans.rows}
                buildStartMs={build.startMs}
                buildDurationMs={build.durationMs}
              />
            </Panel>
          )}
        </>
      )}
    </PluginPage>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  header: css({
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "space-between",
    gap: theme.spacing(2),
    marginBottom: theme.spacing(2),
  }),
  context: css({
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: theme.spacing(2),
  }),
  muted: css({ color: theme.colors.text.secondary }),
});
