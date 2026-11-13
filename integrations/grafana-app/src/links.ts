import { locationService } from "@grafana/runtime";

import { filterKeys } from "./filters";

export const pluginId = "chrismatix-grog-app";
export const appPath = `/a/${pluginId}`;

// Every internal link carries the current filters, so drilling down and back keeps the scope.
export function appUrl(page: string, params: Record<string, string | number> = {}): string {
  const current = locationService.getSearchObject();
  const search = new URLSearchParams();
  for (const key of filterKeys) {
    if (typeof current[key] === "string" && current[key] !== "") {
      search.set(key, String(current[key]));
    }
  }
  for (const [key, value] of Object.entries(params)) {
    search.set(key, String(value));
  }
  const query = search.toString();
  return `${appPath}${page ? `/${page}` : ""}${query ? `?${query}` : ""}`;
}

export interface BuildReference {
  traceId: string;
  startMs: number;
  durationMs: number;
}

export function buildUrl(traceId: string, startMs: number, durationMs: number): string {
  return appUrl("build", {
    trace: traceId,
    start: startMs,
    duration: durationMs,
  });
}

function encodeBuild(build: BuildReference): string {
  return `${build.traceId}:${build.startMs}:${build.durationMs}`;
}

export function decodeBuild(value: unknown): BuildReference | undefined {
  const [traceId, startMs, durationMs] = String(value ?? "").split(":");
  if (!traceId || !startMs) {
    return undefined;
  }
  return {
    traceId,
    startMs: Number(startMs),
    durationMs: Number(durationMs),
  };
}

export function compareUrl(base: BuildReference, head: BuildReference): string {
  return appUrl("compare", {
    base: encodeBuild(base),
    head: encodeBuild(head),
  });
}

export function targetUrl(label: string): string {
  return appUrl("target", { label });
}

export function githubRunUrl(repository: string, runId: string, attempt: string): string {
  return `https://github.com/${repository}/actions/runs/${runId}/attempts/${attempt || 1}`;
}

export function formatDuration(milliseconds: number): string {
  if (!Number.isFinite(milliseconds)) {
    return "—";
  }
  const sign = milliseconds < 0 ? "-" : "";
  const absolute = Math.abs(milliseconds);
  if (absolute < 1000) {
    return `${sign}${Math.round(absolute)} ms`;
  }
  if (absolute < 120_000) {
    return `${sign}${(absolute / 1000).toFixed(1)} s`;
  }
  const minutes = Math.round(absolute / 60_000);
  if (minutes < 120) {
    const seconds = Math.round(absolute / 1000);
    return `${sign}${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  }
  return `${sign}${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

export function formatPercent(fraction: number): string {
  if (!Number.isFinite(fraction)) {
    return "—";
  }
  const percent = fraction * 100;
  return `${percent > 0 && percent < 1 ? percent.toFixed(1) : Math.round(percent)}%`;
}

export function formatCount(value: number): string {
  return Number.isFinite(value) ? Math.round(value).toLocaleString() : "—";
}

export function formatTime(milliseconds: number): string {
  return new Date(milliseconds).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatDay(milliseconds: number): string {
  return new Date(milliseconds).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

// The current page with some query parameters replaced; undefined removes one.
export function withParams(params: Record<string, string | undefined>): string {
  const location = locationService.getLocation();
  const search = new URLSearchParams(location.search);
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") {
      search.delete(key);
    } else {
      search.set(key, value);
    }
  }
  const query = search.toString();
  return `${location.pathname}${query ? `?${query}` : ""}`;
}
