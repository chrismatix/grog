import type { Filters } from "../filters";
import { phases } from "../phases";
import { quote, settings } from "./client";

// Checkouts are detached, so git_branch is often HEAD; prefer GitHub's PR head and ref names.
export const branchExpression = `multiIf(SpanAttributes['grog.tag.github.head_ref'] != '', SpanAttributes['grog.tag.github.head_ref'], SpanAttributes['grog.tag.github.ref_name'] != '', SpanAttributes['grog.tag.github.ref_name'], SpanAttributes['grog.git_branch'])`;

export const mergeQueuePrefix = "gh-readonly-queue/";

// "current" is the selected range; "previous" is the equally long range before it.
export type Period = "current" | "previous" | "both";

export function periodCondition(range: string, period: Period): string {
  if (period === "current") {
    return `Timestamp > now() - INTERVAL ${range}`;
  }
  if (period === "previous") {
    return `Timestamp BETWEEN now() - INTERVAL ${range} - INTERVAL ${range} AND now() - INTERVAL ${range}`;
  }
  return `Timestamp > now() - INTERVAL ${range} - INTERVAL ${range}`;
}

export function isCurrent(range: string): string {
  return `Timestamp > now() - INTERVAL ${range}`;
}

export function bucketExpression(range: string, column = "Timestamp"): string {
  return range === "1 DAY"
    ? `toUnixTimestamp(toStartOfHour(${column})) * 1000`
    : `toUnixTimestamp(toStartOfDay(${column})) * 1000`;
}

export function rootConditions(filters: Filters, period: Period = "current"): string {
  const conditions = [
    `ServiceName = 'grog'`,
    `ParentSpanId = ''`,
    periodCondition(filters.range, period),
  ];
  if (filters.workflow) {
    conditions.push(`SpanAttributes['grog.tag.github.workflow'] = ${quote(filters.workflow)}`);
  }
  if (filters.job) {
    conditions.push(`SpanAttributes['grog.tag.github.job'] = ${quote(filters.job)}`);
  }
  if (filters.branch) {
    conditions.push(
      filters.branchMatch === "equals"
        ? `${branchExpression} = ${quote(filters.branch)}`
        : `positionCaseInsensitive(${branchExpression}, ${quote(filters.branch)}) > 0`,
    );
  }
  if (filters.arch) {
    conditions.push(`ResourceAttributes['host.arch'] = ${quote(`linux/${filters.arch}`)}`);
  }
  const [tagKey, ...tagValue] = filters.tag.split("=");
  if (tagKey) {
    conditions.push(
      `SpanAttributes[${quote(`grog.tag.${tagKey}`)}] = ${quote(tagValue.join("="))}`,
    );
  }
  return conditions.join("\n  AND ");
}

export function rootsCte(filters: Filters, period: Period): string {
  return `roots AS (
  SELECT
    TraceId,
    ${isCurrent(filters.range)} AS is_current,
    toUnixTimestamp64Milli(Timestamp) AS build_start_ms,
    intDiv(Duration, 1000000) AS build_duration_ms,
    SpanAttributes['grog.tag.github.job'] AS job,
    ${branchExpression} AS branch,
    replaceOne(ResourceAttributes['host.arch'], 'linux/', '') AS arch,
    JSONExtract(SpanAttributes['grog.critical_path'], 'Array(String)') AS critical_path,
    mapContains(SpanAttributes, 'grog.critical_path') AS has_critical_path
  FROM ${settings.tracesTable}
  WHERE ${rootConditions(filters, period)}
)`;
}

// Filtered builds plus their target spans; failed targets report CACHE_HIT, so they count as executed.
export function targetSpansCte(filters: Filters, period: Period): string {
  return `${rootsCte(filters, period)},
spans AS (
  SELECT
    t.SpanName AS label,
    t.TraceId AS trace_id,
    t.Timestamp AS timestamp,
    roots.is_current AS is_current,
    roots.build_start_ms AS build_start_ms,
    roots.build_duration_ms AS build_duration_ms,
    roots.has_critical_path AS has_critical_path,
    roots.job AS job,
    roots.branch AS branch,
    roots.arch AS arch,
    has(roots.critical_path, t.SpanName) AS on_critical_path,
    t.StatusCode = 'Error' AS failed,
    t.SpanAttributes['grog.cache_result'] != 'CACHE_HIT' OR failed AS executed,
    toInt64OrZero(t.SpanAttributes['grog.command_duration_ms']) AS command_ms,
    t.SpanAttributes['grog.change_hash'] AS change_hash,
    t.SpanAttributes['grog.tags'] AS tags,
    t.SpanAttributes['grog.dependencies'] AS dependencies,
    t.SpanAttributes['grog.is_test'] = 'true' AS is_test,
    ${phases.map((phase) => `toInt64OrZero(t.SpanAttributes['grog.${phase.key}']) AS ${phase.key}`).join(",\n    ")}
  FROM ${settings.tracesTable} AS t
  INNER JOIN roots ON roots.TraceId = t.TraceId
  WHERE t.ServiceName = 'grog'
    AND t.ParentSpanId != ''
    AND t.${periodCondition(filters.range, period)}
)`;
}
