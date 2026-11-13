import type { UrlQueryMap } from "@grafana/data";

export interface Filters {
  range: string;
  workflow: string;
  job: string;
  branch: string;
  branchMatch: string;
  arch: string;
  // `<tag key>=<value>`, matched against the root span's `grog.tag.<tag key>`.
  tag: string;
}

export const filterKeys = [
  "range",
  "workflow",
  "job",
  "branch",
  "branchMatch",
  "arch",
  "tag",
] as const;

export const rangeOptions = ["1 DAY", "7 DAY", "30 DAY", "90 DAY"];

export function parseFilters(search: UrlQueryMap): Filters {
  const value = (key: string, fallback = "") =>
    typeof search[key] === "string" ? String(search[key]) : fallback;
  return {
    range: rangeOptions.includes(value("range")) ? value("range") : "7 DAY",
    workflow: value("workflow"),
    job: value("job"),
    branch: value("branch"),
    branchMatch: value("branchMatch", "contains"),
    arch: value("arch"),
    tag: value("tag"),
  };
}

export function rangeLabel(range: string): string {
  const days = Number(range.split(" ")[0]);
  return days === 1 ? "24 hours" : `${days} days`;
}
