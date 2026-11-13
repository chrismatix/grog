import { useMemo } from "react";

import type { TargetSpan } from "./components/DependencyGraph";
import type { BuildReference } from "./links";
import { phases } from "./phases";
import { buildRootQuery, buildSpansQuery } from "./sql/builds";
import { useSql } from "./useSql";

function parseList(json: unknown): string[] {
  try {
    return JSON.parse(String(json || "[]"));
  } catch {
    return [];
  }
}

export function isExecuted(span: TargetSpan): boolean {
  return span.cacheResult !== "CACHE_HIT" || span.failed;
}

export function useBuild(build: BuildReference) {
  const root = useSql(buildRootQuery(build.traceId, build.startMs, build.durationMs));
  const spanRows = useSql(buildSpansQuery(build.traceId, build.startMs, build.durationMs));
  const attributes: Record<string, string> = useMemo(
    () => (root.rows[0] ? JSON.parse(String(root.rows[0].attributes)) : {}),
    [root.rows],
  );
  const criticalPath = useMemo(() => parseList(attributes["grog.critical_path"]), [attributes]);
  const spans: TargetSpan[] = useMemo(
    () =>
      spanRows.rows.map((row) => ({
        label: String(row.label),
        startMs: Number(row.start_ms),
        durationMs: Number(row.duration_ms),
        commandMs: Number(row.command_duration_ms),
        cacheResult: String(row.cache_result),
        failed: Boolean(Number(row.failed)),
        dependencies: parseList(row.dependencies),
        phases: Object.fromEntries(phases.map((phase) => [phase.key, Number(row[phase.key])])),
      })),
    [spanRows.rows],
  );
  return {
    root,
    spanRows,
    loading: root.loading || spanRows.loading,
    error: root.error ?? spanRows.error,
    command: String(root.rows[0]?.command ?? "Build"),
    arch: String(root.rows[0]?.arch ?? ""),
    attributes,
    criticalPath,
    spans,
  };
}

export type Build = ReturnType<typeof useBuild>;

export function branchOf(attributes: Record<string, string>): string {
  return (
    attributes["grog.tag.github.head_ref"] ||
    attributes["grog.tag.github.ref_name"] ||
    attributes["grog.git_branch"] ||
    ""
  );
}

// Longest dependency chain, weighting each target by the time it occupied a worker.
function longestChain(spans: TargetSpan[], weightOf: (span: TargetSpan) => number): number {
  const byLabel = new Map(spans.map((span) => [span.label, span]));
  const finish = new Map<string, number>();
  const visit = (span: TargetSpan): number => {
    const known = finish.get(span.label);
    if (known !== undefined) {
      return known;
    }
    finish.set(span.label, 0);
    const ready = Math.max(
      0,
      ...span.dependencies
        .map((label) => byLabel.get(label))
        .filter((dependency) => dependency !== undefined)
        .map(visit),
    );
    const done = ready + weightOf(span);
    finish.set(span.label, done);
    return done;
  };
  return Math.max(0, ...spans.map(visit));
}

const occupied = (span: TargetSpan) =>
  Math.max(span.durationMs - (span.phases.queue_wait_ms ?? 0), 0);

export interface CriticalPathSaving {
  label: string;
  occupiedMs: number;
  savingMs: number;
}

// For each critical-path target: how much shorter the chain gets if it took no time.
export function criticalPathSavings(
  spans: TargetSpan[],
  criticalPath: string[],
): CriticalPathSaving[] {
  const baseline = longestChain(spans, occupied);
  return criticalPath
    .map((label) => spans.find((span) => span.label === label))
    .filter((span) => span !== undefined)
    .map((target) => ({
      label: target.label,
      occupiedMs: occupied(target),
      savingMs:
        baseline -
        longestChain(spans, (span) => (span.label === target.label ? 0 : occupied(span))),
    }));
}
