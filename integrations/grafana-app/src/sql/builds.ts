import type { Filters } from "../filters";
import { phases } from "../phases";
import { quote, settings } from "./client";
import {
  branchExpression,
  bucketExpression,
  isCurrent,
  mergeQueuePrefix,
  rootConditions,
  rootsCte,
} from "./scope";

const executedTargets = `toUInt32OrZero(SpanAttributes['grog.total_targets']) - toUInt32OrZero(SpanAttributes['grog.cache_hit_count'])`;

const buildMetrics = `count() AS builds,
  quantile(0.5)(Duration) / 1e6 AS duration_p50_ms,
  quantile(0.95)(Duration) / 1e6 AS duration_p95_ms,
  avg(Duration) / 1e6 AS duration_mean_ms,
  quantileIf(0.5)(toInt64OrZero(SpanAttributes['grog.critical_path_exec_ms']), mapContains(SpanAttributes, 'grog.critical_path')) AS critical_path_p50_ms,
  quantile(0.5)(${executedTargets}) AS executed_p50,
  countIf(toUInt32OrZero(SpanAttributes['grog.failure_count']) > 0) / count() AS failure_rate,
  countIf(toUInt32OrZero(SpanAttributes['grog.tag.github.run_attempt']) > 1) / count() AS rerun_rate,
  sum(toUInt32OrZero(SpanAttributes['grog.cache_hit_count'])) / greatest(sum(toUInt32OrZero(SpanAttributes['grog.total_targets'])), 1) AS hit_rate,
  quantileIf(0.5)(toInt64OrZero(SpanAttributes['grog.async_cache_wait_ms']), mapContains(SpanAttributes, 'grog.async_cache_wait_ms')) AS async_cache_wait_p50_ms,
  sum(Duration) / 1e6 AS total_ms`;

export function kpiQuery(filters: Filters): string {
  return `SELECT
  ${isCurrent(filters.range)} AS is_current,
  ${buildMetrics}
FROM ${settings.tracesTable}
WHERE ${rootConditions(filters, "both")}
GROUP BY is_current`;
}

export function trendQuery(filters: Filters): string {
  return `SELECT
  ${bucketExpression(filters.range)} AS bucket_ms,
  ${buildMetrics}
FROM ${settings.tracesTable}
WHERE ${rootConditions(filters)}
GROUP BY bucket_ms
ORDER BY bucket_ms`;
}

export const breakdownDimensions = {
  job: {
    label: "Workflow / job",
    expression: `concat(SpanAttributes['grog.tag.github.workflow'], ' / ', SpanAttributes['grog.tag.github.job'])`,
  },
  branch: {
    label: "Branch",
    expression: `if(startsWith(${branchExpression}, '${mergeQueuePrefix}'), '${mergeQueuePrefix}', ${branchExpression})`,
  },
  event: {
    label: "Event",
    expression: `SpanAttributes['grog.tag.github.event_name']`,
  },
  arch: {
    label: "Arch",
    expression: `replaceOne(ResourceAttributes['host.arch'], 'linux/', '')`,
  },
} as const;

export type BreakdownDimension = keyof typeof breakdownDimensions;

export function breakdownQuery(filters: Filters, dimension: BreakdownDimension): string {
  return `SELECT
  ${breakdownDimensions[dimension].expression} AS key,
  ${buildMetrics}
FROM ${settings.tracesTable}
WHERE ${rootConditions(filters)}
GROUP BY key
ORDER BY builds DESC
LIMIT 25`;
}

export function phaseTotalsQuery(filters: Filters): string {
  return `WITH ${rootsCte(filters, "current")}
SELECT
  ${phases.map((phase) => `sum(toInt64OrZero(t.SpanAttributes['grog.${phase.key}'])) AS ${phase.key}`).join(",\n  ")}
FROM ${settings.tracesTable} AS t
WHERE t.ServiceName = 'grog'
  AND t.ParentSpanId != ''
  AND t.${isCurrent(filters.range)}
  AND t.TraceId IN (SELECT TraceId FROM roots)`;
}

export const pageSize = 200;

export function buildsQuery(filters: Filters, status: string, limit: number): string {
  const statusCondition =
    status === "failed"
      ? `AND toUInt32OrZero(SpanAttributes['grog.failure_count']) > 0`
      : status === "rerun"
        ? `AND toUInt32OrZero(SpanAttributes['grog.tag.github.run_attempt']) > 1`
        : "";
  return `SELECT
  TraceId AS trace_id,
  toUnixTimestamp64Milli(Timestamp) AS start_ms,
  intDiv(Duration, 1000000) AS duration_ms,
  SpanName AS command,
  SpanAttributes['grog.tag.github.workflow'] AS workflow,
  SpanAttributes['grog.tag.github.job'] AS job,
  ${branchExpression} AS branch,
  SpanAttributes['grog.tag.github.repository'] AS repository,
  SpanAttributes['grog.tag.github.run_id'] AS run_id,
  SpanAttributes['grog.tag.github.run_attempt'] AS run_attempt,
  replaceOne(ResourceAttributes['host.arch'], 'linux/', '') AS arch,
  toUInt32OrZero(SpanAttributes['grog.total_targets']) AS targets,
  ${executedTargets} AS executed,
  toUInt32OrZero(SpanAttributes['grog.failure_count']) AS failures,
  toInt64OrZero(SpanAttributes['grog.critical_path_exec_ms']) AS critical_path_ms,
  count() OVER () AS matching
FROM ${settings.tracesTable}
WHERE ${rootConditions(filters)}
  ${statusCondition}
ORDER BY Timestamp DESC
LIMIT ${limit}`;
}

export function filterValuesQuery(range: string): string {
  return `SELECT
  groupUniqArray(SpanAttributes['grog.tag.github.workflow']) AS workflows,
  groupUniqArray(SpanAttributes['grog.tag.github.job']) AS jobs,
  groupUniqArray(replaceOne(ResourceAttributes['host.arch'], 'linux/', '')) AS arches
FROM ${settings.tracesTable}
WHERE ServiceName = 'grog' AND ParentSpanId = '' AND Timestamp > now() - INTERVAL ${range}`;
}

export function tagValuesQuery(range: string): string {
  return `SELECT
  substring(key, 10) AS tag,
  groupUniqArray(50)(SpanAttributes[key]) AS tag_values
FROM ${settings.tracesTable}
ARRAY JOIN arrayFilter(key -> startsWith(key, 'grog.tag.'), mapKeys(SpanAttributes)) AS key
WHERE ServiceName = 'grog' AND ParentSpanId = '' AND Timestamp > now() - INTERVAL ${range}
  AND key NOT IN ('grog.tag.github.workflow', 'grog.tag.github.job', 'grog.tag.github.run_id', 'grog.tag.github.run_attempt', 'grog.tag.github.head_ref', 'grog.tag.github.repository')
GROUP BY tag
ORDER BY tag`;
}

// Bounded by the build's own time window: the TraceId index table has corrupt timestamps.
function traceWindow(traceId: string, startMs: number, durationMs: number): string {
  return `ServiceName = 'grog'
  AND TraceId = ${quote(traceId)}
  AND Timestamp BETWEEN fromUnixTimestamp64Milli(${startMs - 60_000}) AND fromUnixTimestamp64Milli(${startMs + durationMs + 60_000})`;
}

export function buildRootQuery(traceId: string, startMs: number, durationMs: number): string {
  return `SELECT
  SpanName AS command,
  toUnixTimestamp64Milli(Timestamp) AS start_ms,
  intDiv(Duration, 1000000) AS duration_ms,
  toJSONString(SpanAttributes) AS attributes,
  replaceOne(ResourceAttributes['host.arch'], 'linux/', '') AS arch
FROM ${settings.tracesTable}
WHERE ${traceWindow(traceId, startMs, durationMs)} AND ParentSpanId = ''`;
}

export function buildSpansQuery(traceId: string, startMs: number, durationMs: number): string {
  return `SELECT
  SpanName AS label,
  toUnixTimestamp64Milli(Timestamp) AS start_ms,
  intDiv(Duration, 1000000) AS duration_ms,
  StatusCode = 'Error' AS failed,
  SpanAttributes['grog.cache_result'] AS cache_result,
  SpanAttributes['grog.dependencies'] AS dependencies,
  ${phases.map((phase) => `toInt64OrZero(SpanAttributes['grog.${phase.key}']) AS ${phase.key}`).join(",\n  ")}
FROM ${settings.tracesTable}
WHERE ${traceWindow(traceId, startMs, durationMs)} AND ParentSpanId != ''`;
}

export function previousBuildQuery(
  startMs: number,
  workflow: string,
  job: string,
  branch: string,
): string {
  return `SELECT
  TraceId AS trace_id,
  toUnixTimestamp64Milli(Timestamp) AS start_ms,
  intDiv(Duration, 1000000) AS duration_ms
FROM ${settings.tracesTable}
WHERE ServiceName = 'grog'
  AND ParentSpanId = ''
  AND Timestamp BETWEEN fromUnixTimestamp64Milli(${startMs}) - INTERVAL 30 DAY AND fromUnixTimestamp64Milli(${startMs - 1})
  AND SpanAttributes['grog.tag.github.workflow'] = ${quote(workflow)}
  AND SpanAttributes['grog.tag.github.job'] = ${quote(job)}
  AND ${branchExpression} = ${quote(branch)}
ORDER BY Timestamp DESC
LIMIT 1`;
}

export function dependentsQuery(
  label: string,
  traceId: string,
  startMs: number,
  durationMs: number,
): string {
  return `SELECT DISTINCT SpanName AS label
FROM ${settings.tracesTable}
WHERE ${traceWindow(traceId, startMs, durationMs)}
  AND has(JSONExtract(SpanAttributes['grog.dependencies'], 'Array(String)'), ${quote(label)})
ORDER BY label`;
}

export function rerunsQuery(filters: Filters): string {
  return `SELECT
  SpanAttributes['grog.tag.github.workflow'] AS workflow,
  SpanAttributes['grog.tag.github.job'] AS job,
  uniqExact(SpanAttributes['grog.tag.github.run_id']) AS runs,
  uniqExactIf(SpanAttributes['grog.tag.github.run_id'], toUInt32OrZero(SpanAttributes['grog.tag.github.run_attempt']) > 1) AS rerun_runs,
  rerun_runs / greatest(runs, 1) AS rerun_rate
FROM ${settings.tracesTable}
WHERE ${rootConditions(filters)}
  AND SpanAttributes['grog.tag.github.run_id'] != ''
GROUP BY workflow, job
ORDER BY rerun_runs DESC, rerun_rate DESC`;
}
