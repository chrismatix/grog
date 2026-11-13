import type { Filters } from "../filters";
import { quote, settings } from "./client";
import { bucketExpression, isCurrent, rootConditions } from "./scope";

const jobBranch = `if(SpanAttributes['github.head_ref'] != '', SpanAttributes['github.head_ref'], SpanAttributes['github.ref_name'])`;

// GitHub Actions job spans from examples/github-job-spans.sh; arch and grog tags do not apply.
function jobConditions(filters: Filters): string {
  const conditions = [
    `ServiceName = 'github-actions'`,
    `ParentSpanId = ''`,
    isCurrent(filters.range),
  ];
  if (filters.workflow) {
    conditions.push(`SpanAttributes['github.workflow'] = ${quote(filters.workflow)}`);
  }
  if (filters.job) {
    conditions.push(`SpanAttributes['github.job'] = ${quote(filters.job)}`);
  }
  if (filters.branch) {
    conditions.push(
      filters.branchMatch === "equals"
        ? `${jobBranch} = ${quote(filters.branch)}`
        : `positionCaseInsensitive(${jobBranch}, ${quote(filters.branch)}) > 0`,
    );
  }
  return conditions.join("\n  AND ");
}

const jobsCte = (filters: Filters) => `grog AS (
  SELECT
    SpanAttributes['grog.tag.github.run_id'] AS run_id,
    SpanAttributes['grog.tag.github.run_attempt'] AS run_attempt,
    SpanAttributes['grog.tag.github.job'] AS job,
    sum(Duration) / 1e6 AS grog_ms
  FROM ${settings.tracesTable}
  WHERE ${rootConditions({ ...filters, arch: "", tag: "" })}
  GROUP BY run_id, run_attempt, job
),
jobs AS (
  SELECT
    SpanAttributes['github.workflow'] AS workflow,
    SpanAttributes['github.job'] AS job,
    SpanAttributes['github.run_id'] AS run_id,
    SpanAttributes['github.run_attempt'] AS run_attempt,
    Timestamp,
    Duration / 1e6 AS duration_ms,
    toInt64OrZero(SpanAttributes['github.queue_ms']) AS queue_ms,
    StatusCode = 'Error' AS failed,
    count() OVER (PARTITION BY run_id, run_attempt, job) AS matrix_size
  FROM ${settings.tracesTable}
  WHERE ${jobConditions(filters)}
)`;

// Matrix jobs share one job key, so their grog time is split evenly between them.
export function jobsSummaryQuery(filters: Filters): string {
  return `WITH ${jobsCte(filters)}
SELECT
  jobs.workflow AS workflow,
  jobs.job AS job,
  count() AS runs,
  quantile(0.5)(queue_ms) AS queue_p50_ms,
  quantile(0.95)(queue_ms) AS queue_p95_ms,
  quantile(0.5)(duration_ms) AS duration_p50_ms,
  quantile(0.95)(duration_ms) AS duration_p95_ms,
  quantile(0.5)(grog.grog_ms / matrix_size) AS grog_p50_ms,
  avg(failed) AS failure_rate,
  sum(duration_ms) AS total_ms,
  sum(queue_ms) AS total_queue_ms
FROM jobs
LEFT JOIN grog ON grog.run_id = jobs.run_id AND grog.run_attempt = jobs.run_attempt AND grog.job = jobs.job
GROUP BY workflow, job
ORDER BY total_ms DESC`;
}

export function jobsTrendQuery(filters: Filters): string {
  return `SELECT
  ${bucketExpression(filters.range)} AS bucket_ms,
  quantile(0.5)(toInt64OrZero(SpanAttributes['github.queue_ms'])) AS queue_p50_ms,
  quantile(0.95)(toInt64OrZero(SpanAttributes['github.queue_ms'])) AS queue_p95_ms,
  quantile(0.5)(Duration / 1e6) AS duration_p50_ms
FROM ${settings.tracesTable}
WHERE ${jobConditions(filters)}
GROUP BY bucket_ms
ORDER BY bucket_ms`;
}

export function jobStepsQuery(filters: Filters, workflow: string, job: string): string {
  return `SELECT
  SpanName AS step,
  count() AS runs,
  quantile(0.5)(Duration / 1e6) AS p50_ms,
  quantile(0.95)(Duration / 1e6) AS p95_ms,
  sum(Duration / 1e6) AS total_ms,
  countIf(StatusCode = 'Error') AS failures,
  avg(toUInt32OrZero(SpanAttributes['github.step_number'])) AS position
FROM ${settings.tracesTable}
WHERE ServiceName = 'github-actions'
  AND ParentSpanId != ''
  AND ${isCurrent(filters.range)}
  AND TraceId IN (
    SELECT TraceId FROM ${settings.tracesTable}
    WHERE ${jobConditions({ ...filters, workflow, job })}
  )
GROUP BY step
ORDER BY position`;
}

// The job span whose run, attempt and job key match the build and that was running when it started.
export function buildJobQuery(
  runId: string,
  runAttempt: string,
  job: string,
  buildStartMs: number,
): string {
  const window = `Timestamp BETWEEN fromUnixTimestamp64Milli(${buildStartMs}) - INTERVAL 1 DAY AND fromUnixTimestamp64Milli(${buildStartMs}) + INTERVAL 1 DAY`;
  return `SELECT
  SpanName AS name,
  ParentSpanId = '' AS is_job,
  toUnixTimestamp64Milli(Timestamp) AS start_ms,
  intDiv(Duration, 1000000) AS duration_ms,
  StatusCode = 'Error' AS failed,
  toInt64OrZero(SpanAttributes['github.queue_ms']) AS queue_ms,
  SpanAttributes['github.runner_labels'] AS runner_labels
FROM ${settings.tracesTable}
WHERE ServiceName = 'github-actions'
  AND ${window}
  AND TraceId IN (
    SELECT TraceId FROM ${settings.tracesTable}
    WHERE ServiceName = 'github-actions'
      AND ParentSpanId = ''
      AND ${window}
      AND SpanAttributes['github.run_id'] = ${quote(runId)}
      AND SpanAttributes['github.run_attempt'] = ${quote(runAttempt)}
      AND SpanAttributes['github.job'] = ${quote(job)}
      AND Timestamp <= fromUnixTimestamp64Milli(${buildStartMs})
      AND toUnixTimestamp64Milli(Timestamp) + intDiv(Duration, 1000000) >= ${buildStartMs}
    ORDER BY Timestamp DESC
    LIMIT 1
  )
ORDER BY Timestamp`;
}
