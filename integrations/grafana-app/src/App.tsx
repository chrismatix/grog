import type { AppRootProps } from "@grafana/data";
import { locationService } from "@grafana/runtime";
import { Alert, TextLink } from "@grafana/ui";
import { useEffect, useState } from "react";

import { parseFilters } from "./filters";
import { appPath, decodeBuild, pluginId } from "./links";
import { BuildPage } from "./pages/BuildPage";
import { BuildsPage } from "./pages/BuildsPage";
import { CachePage } from "./pages/CachePage";
import { ComparePage } from "./pages/ComparePage";
import { JobsPage } from "./pages/JobsPage";
import { OverviewPage } from "./pages/OverviewPage";
import { RegressionsPage } from "./pages/RegressionsPage";
import { ReliabilityPage } from "./pages/ReliabilityPage";
import { TargetPage } from "./pages/TargetPage";
import { TargetsPage } from "./pages/TargetsPage";
import { pageSize } from "./sql/builds";
import { type AppSettings, defaultTracesTable, settings } from "./sql/client";

export function App({ meta }: AppRootProps<AppSettings>) {
  settings.datasourceUid = meta.jsonData?.datasourceUid ?? "";
  settings.tracesTable = meta.jsonData?.tracesTable || defaultTracesTable;
  const [location, setLocation] = useState(() => ({
    path: locationService.getLocation().pathname,
    search: locationService.getSearchObject(),
  }));
  useEffect(
    () =>
      locationService.getHistory().listen(() =>
        setLocation({
          path: locationService.getLocation().pathname,
          search: locationService.getSearchObject(),
        }),
      ),
    [],
  );

  if (!settings.datasourceUid) {
    return (
      <Alert severity="info" title="Pick a ClickHouse datasource">
        Choose the datasource with grog's traces on the{" "}
        <TextLink href={`/plugins/${pluginId}`}>configuration page</TextLink>.
      </Alert>
    );
  }

  const { search } = location;
  const page = location.path.replace(appPath, "").replace(/^\//, "").split("/")[0];
  const filters = parseFilters(search);
  const text = (key: string) => (typeof search[key] === "string" ? String(search[key]) : "");

  switch (page) {
    case "build":
      return (
        <BuildPage
          build={{
            traceId: text("trace"),
            startMs: Number(search.start),
            durationMs: Number(search.duration),
          }}
        />
      );
    case "compare": {
      const base = decodeBuild(search.base);
      const head = decodeBuild(search.head);
      return base && head ? <ComparePage base={base} head={head} /> : null;
    }
    case "target":
      return <TargetPage label={text("label")} filters={filters} />;
    case "regressions":
      return (
        <RegressionsPage
          filters={filters}
          direction={text("direction") === "faster" ? "faster" : "slower"}
        />
      );
    case "targets":
      return (
        <TargetsPage
          filters={filters}
          search={text("q")}
          sort={text("sort") === "critical" ? "critical" : "total"}
        />
      );
    case "cache":
      return <CachePage filters={filters} />;
    case "reliability":
      return <ReliabilityPage filters={filters} />;
    case "builds":
      return (
        <BuildsPage
          filters={filters}
          status={text("status")}
          limit={Number(search.limit) || pageSize}
        />
      );
    case "jobs":
      return <JobsPage filters={filters} selectedJob={text("selectedJob")} />;
    default:
      return <OverviewPage filters={filters} />;
  }
}
