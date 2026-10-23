import type { Filters } from "../filters";
import { cachePhaseKeys } from "../phases";
import { quote } from "./client";
import { bucketExpression, targetSpansCte } from "./scope";

export type TargetOrder = "total" | "critical" | "slower" | "faster";

const orderings: Record<TargetOrder, string> = {
  total: "total_ms DESC",
  critical: "critical_ms DESC",
  slower: "impact_ms DESC",
  faster: "impact_ms ASC",
};

// Regressions need enough executions in both periods for the medians to mean anything.
export const minimumExecutions = 5;

export function targetsQuery(
  filters: Filters,
  order: TargetOrder,
  limit: number,
  search = "",
): string {
  const having = ["runs > 0"];
  if (order === "slower" || order === "faster") {
    having.push(
      `executed_runs >= ${minimumExecutions}`,
      `previous_executed_runs >= ${minimumExecutions}`,
      order === "slower" ? "impact_ms > 0" : "impact_ms < 0",
    );
  }
  return `WITH ${targetSpansCte(filters, "both")}
SELECT
  label,
  argMax(tags, timestamp) AS latest_tags,
  countIf(is_current) AS runs,
  countIf(is_current AND executed) AS executed_runs,
  countIf(NOT is_current AND executed) AS previous_executed_runs,
  1 - executed_runs / greatest(runs, 1) AS hit_rate,
  quantileIf(0.5)(command_ms, is_current AND executed) AS p50_ms,
  quantileIf(0.5)(command_ms, NOT is_current AND executed) AS previous_p50_ms,
  quantileIf(0.95)(command_ms, is_current AND executed) AS p95_ms,
  sumIf(command_ms, is_current AND executed) AS total_ms,
  sumIf(command_ms, is_current AND on_critical_path) AS critical_ms,
  countIf(is_current AND on_critical_path) AS critical_path_runs,
  (SELECT countIf(is_current AND has_critical_path) FROM roots) AS builds_with_critical_path,
  countIf(is_current AND failed) AS failures,
  (p50_ms - previous_p50_ms) * executed_runs AS impact_ms
FROM spans
${search ? `WHERE positionCaseInsensitive(label, ${quote(search)}) > 0` : ""}
GROUP BY label
HAVING ${having.join(" AND ")}
ORDER BY ${orderings[order]}
LIMIT ${limit}`;
}

export function targetTrendsQuery(filters: Filters, labels: string[]): string {
  return `WITH ${targetSpansCte(filters, "both")}
SELECT
  label,
  ${bucketExpression(filters.range, "timestamp")} AS bucket_ms,
  quantile(0.5)(command_ms) AS p50_ms
FROM spans
WHERE executed AND label IN (${labels.map(quote).join(", ") || "''"})
GROUP BY label, bucket_ms
ORDER BY bucket_ms`;
}

export function targetRunsQuery(label: string, filters: Filters): string {
  return `WITH ${targetSpansCte(filters, "both")}
SELECT
  trace_id,
  toUnixTimestamp64Milli(timestamp) AS start_ms,
  is_current,
  command_ms,
  executed,
  failed,
  change_hash,
  tags,
  dependencies,
  build_start_ms,
  build_duration_ms,
  job,
  branch,
  arch,
  on_critical_path
FROM spans
WHERE label = ${quote(label)}
ORDER BY timestamp`;
}

// Successful executions of inputs that already executed successfully: the cache should have hit.
export function repeatedExecutionsQuery(filters: Filters, limit: number): string {
  return `WITH ${targetSpansCte(filters, "current")},
by_hash AS (
  SELECT
    label,
    change_hash,
    count() AS executions,
    sum(command_ms) - argMin(command_ms, timestamp) AS repeat_ms,
    argMax(tags, timestamp) AS hash_tags,
    max(timestamp) AS last_seen
  FROM spans
  WHERE executed AND NOT failed
  GROUP BY label, change_hash
)
SELECT
  label,
  argMax(hash_tags, last_seen) AS latest_tags,
  sum(executions) AS total_executions,
  count() AS input_hashes,
  total_executions - input_hashes AS repeats,
  sum(repeat_ms) AS total_repeat_ms,
  sum(sum(repeat_ms)) OVER () AS all_repeat_ms,
  sum(sum(executions) - count()) OVER () AS all_repeats,
  sum(sum(executions)) OVER () AS all_executions
FROM by_hash
GROUP BY label
HAVING repeats > 0
ORDER BY total_repeat_ms DESC
LIMIT ${limit}`;
}

export function cacheOverheadQuery(filters: Filters, limit: number): string {
  return `WITH ${targetSpansCte(filters, "current")}
SELECT
  label,
  count() AS runs,
  ${cachePhaseKeys.map((key) => `sum(${key}) AS total_${key}`).join(",\n  ")},
  ${cachePhaseKeys.map((key) => `sum(${key})`).join(" + ")} AS overhead_ms
FROM spans
GROUP BY label
ORDER BY overhead_ms DESC
LIMIT ${limit}`;
}

export function flakyTargetsQuery(filters: Filters, limit: number): string {
  return `WITH ${targetSpansCte(filters, "current")},
by_hash AS (
  SELECT
    label,
    change_hash,
    countIf(failed) AS failures,
    countIf(executed AND NOT failed) AS passes,
    maxIf(timestamp, failed) AS last_failure,
    argMaxIf(trace_id, timestamp, failed) AS failure_trace_id,
    argMaxIf(build_start_ms, timestamp, failed) AS failure_build_start_ms,
    argMaxIf(build_duration_ms, timestamp, failed) AS failure_build_duration_ms,
    max(is_test) AS hash_is_test
  FROM spans
  GROUP BY label, change_hash
  HAVING failures > 0 AND passes > 0
)
SELECT
  label,
  max(hash_is_test) AS test,
  count() AS flaky_hashes,
  sum(failures) AS total_failures,
  sum(passes) AS total_passes,
  toUnixTimestamp64Milli(max(last_failure)) AS last_failure_ms,
  argMax(failure_trace_id, last_failure) AS last_failure_trace_id,
  argMax(failure_build_start_ms, last_failure) AS last_failure_build_start_ms,
  argMax(failure_build_duration_ms, last_failure) AS last_failure_build_duration_ms,
  count() OVER () AS all_flaky_targets
FROM by_hash
GROUP BY label
ORDER BY flaky_hashes DESC, total_failures DESC
LIMIT ${limit}`;
}

export function failingTargetsQuery(filters: Filters, limit: number): string {
  return `WITH ${targetSpansCte(filters, "current")}
SELECT
  label,
  countIf(failed) AS failures,
  countIf(executed) AS executed_runs,
  failures / greatest(executed_runs, 1) AS failure_rate,
  uniqExactIf(change_hash, failed) AS failing_hashes,
  toUnixTimestamp64Milli(maxIf(timestamp, failed)) AS last_failure_ms,
  argMaxIf(trace_id, timestamp, failed) AS last_failure_trace_id,
  argMaxIf(build_start_ms, timestamp, failed) AS last_failure_build_start_ms,
  argMaxIf(build_duration_ms, timestamp, failed) AS last_failure_build_duration_ms,
  count() OVER () AS all_failing_targets
FROM spans
GROUP BY label
HAVING failures > 0
ORDER BY failures DESC
LIMIT ${limit}`;
}
