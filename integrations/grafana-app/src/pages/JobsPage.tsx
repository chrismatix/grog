import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { Alert, TextLink, useStyles2, useTheme2 } from "@grafana/ui";

import { countColumn, durationColumn, percentColumn } from "../columns";
import { AppPage } from "../components/AppPage";
import { DataTable } from "../components/DataTable";
import { Panel } from "../components/Panel";
import { TrendChart } from "../components/TrendChart";
import type { Filters } from "../filters";
import { formatDuration, withParams } from "../links";
import type { Row } from "../sql/client";
import { jobStepsQuery, jobsSummaryQuery, jobsTrendQuery } from "../sql/jobs";
import { useSql } from "../useSql";

const anatomy = [
  { key: "queue", label: "Waiting for a runner", color: "purple" },
  { key: "grog", label: "grog", color: "blue" },
  { key: "other", label: "Other steps", color: "orange" },
] as const;

function anatomyOf(row: Row): Record<string, number> {
  const queue = Number(row.queue_p50_ms);
  const duration = Number(row.duration_p50_ms);
  const grog = Math.min(Number(row.grog_p50_ms), duration);
  return { queue, grog, other: Math.max(duration - grog, 0) };
}

export function JobsPage({ filters, selectedJob }: { filters: Filters; selectedJob: string }) {
  const styles = useStyles2(getStyles);
  const theme = useTheme2();
  const jobs = useSql(jobsSummaryQuery(filters));
  const trend = useSql(jobsTrendQuery(filters));
  const [workflow, job] = selectedJob.split("|");
  const steps = useSql(selectedJob ? jobStepsQuery(filters, workflow, job) : undefined);
  const selectUrl = (row: Row) =>
    withParams({
      selectedJob:
        `${row.workflow}|${row.job}` === selectedJob ? undefined : `${row.workflow}|${row.job}`,
    });
  const longest = Math.max(
    ...jobs.rows.map((row) => {
      const parts = anatomyOf(row);
      return parts.queue + parts.grog + parts.other;
    }),
    1,
  );

  return (
    <AppPage page="jobs" filters={filters}>
      {(filters.arch || filters.tag) && (
        <Alert severity="info" title="">
          The arch and tag filters only apply to grog builds, not to GitHub jobs.
        </Alert>
      )}
      <Panel
        title="Where job time goes"
        help="Median per GitHub Actions job: time queued for a runner, time inside grog, and every other step (checkout, setup, caches, uploads). Click a row to see its steps."
        state={jobs}
        empty="No job timings yet. Export them with the github-job-spans.sh example from this app's README; workflows need the actions: read permission."
      >
        <ul className={styles.legend}>
          {anatomy.map((part) => (
            <li key={part.key}>
              <span
                className={styles.swatch}
                style={{
                  background: theme.visualization.getColorByName(part.color),
                }}
              />
              {part.label}
            </li>
          ))}
        </ul>
        <DataTable
          rows={jobs.rows}
          rowKey={(row) => `${row.workflow}|${row.job}`}
          sortedBy="Total"
          columns={[
            {
              label: "Workflow / job",
              render: (row) => (
                <TextLink
                  href={selectUrl(row)}
                  inline={false}
                  weight={`${row.workflow}|${row.job}` === selectedJob ? "bold" : undefined}
                >
                  {`${row.workflow} / ${row.job}`}
                </TextLink>
              ),
            },
            countColumn("Runs", "runs"),
            {
              label: "Median anatomy",
              render: (row) => {
                const parts = anatomyOf(row);
                return (
                  <div
                    className={styles.anatomy}
                    title={anatomy
                      .map((part) => `${part.label}: ${formatDuration(parts[part.key])}`)
                      .join("\n")}
                  >
                    {anatomy.map((part) => (
                      <div
                        key={part.key}
                        style={{
                          width: `${(parts[part.key] / longest) * 100}%`,
                          background: theme.visualization.getColorByName(part.color),
                        }}
                      />
                    ))}
                  </div>
                );
              },
            },
            durationColumn("Queue p50", "queue_p50_ms"),
            durationColumn("Queue p95", "queue_p95_ms"),
            durationColumn("Job p50", "duration_p50_ms"),
            durationColumn("Job p95", "duration_p95_ms"),
            durationColumn("grog p50", "grog_p50_ms"),
            percentColumn("Failed", (row) => Number(row.failure_rate)),
            durationColumn("Total", "total_ms"),
          ]}
        />
      </Panel>

      {selectedJob && (
        <Panel
          title={`Steps · ${workflow} / ${job}`}
          help="Steps that finished before the export step ran; post-job cleanup is not included"
          state={steps}
          empty="No steps recorded for this job."
        >
          <DataTable
            rows={steps.rows}
            rowKey={(row) => String(row.step)}
            columns={[
              { label: "Step", render: (row) => String(row.step) },
              countColumn("Runs", "runs"),
              durationColumn("Median", "p50_ms"),
              durationColumn("p95", "p95_ms"),
              durationColumn("Total", "total_ms"),
              countColumn("Failures", "failures"),
            ]}
          />
        </Panel>
      )}

      {trend.rows.length > 0 && (
        <Panel title="Queue time and job duration" state={trend}>
          <TrendChart
            rows={trend.rows}
            series={[
              { key: "queue_p50_ms", label: "Queue median", unit: "ms" },
              { key: "queue_p95_ms", label: "Queue p95", unit: "ms" },
              { key: "duration_p50_ms", label: "Job median", unit: "ms" },
            ]}
          />
        </Panel>
      )}
    </AppPage>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  legend: css({
    listStyle: "none",
    display: "flex",
    gap: theme.spacing(2),
    padding: 0,
    margin: theme.spacing(0, 0, 1),
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
    li: { display: "flex", alignItems: "center", gap: theme.spacing(0.75) },
  }),
  swatch: css({ width: 10, height: 10, borderRadius: 2 }),
  anatomy: css({
    display: "flex",
    width: 220,
    height: 12,
    background: theme.colors.background.secondary,
    borderRadius: 2,
    overflow: "hidden",
  }),
});
