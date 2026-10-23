import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { locationService } from "@grafana/runtime";
import { Icon, Input, RadioButtonGroup, useStyles2 } from "@grafana/ui";
import { useEffect, useState } from "react";

import {
  changeColumn,
  countColumn,
  criticalShareColumn,
  criticalTimeColumn,
  durationColumn,
  percentColumn,
  targetColumn,
} from "../columns";
import { AppPage } from "../components/AppPage";
import { DataTable } from "../components/DataTable";
import { Panel } from "../components/Panel";
import type { Filters } from "../filters";
import { targetsQuery } from "../sql/targets";
import { useSql } from "../useSql";

const sortOptions = [
  { label: "Total time", value: "total", column: "Total time" },
  {
    label: "Critical path",
    value: "critical",
    column: "Critical path / build",
  },
] as const;

export function TargetsPage({
  filters,
  search,
  sort,
}: {
  filters: Filters;
  search: string;
  sort: "total" | "critical";
}) {
  const styles = useStyles2(getStyles);
  const [searchDraft, setSearchDraft] = useState(search);
  useEffect(() => setSearchDraft(search), [search]);
  const targets = useSql(targetsQuery(filters, sort, 200, search));
  const setSearch = (value: string) => locationService.partial({ q: value || undefined }, true);

  return (
    <AppPage page="targets" filters={filters}>
      <Panel
        title="Targets"
        help="Every target that ran in a matching build. Times are command times of executed runs; cache hits cost nothing here."
        actions={
          <div className={styles.actions}>
            <Input
              width={36}
              prefix={<Icon name="search" />}
              placeholder="Filter targets, e.g. //frontend"
              value={searchDraft}
              onChange={(event) => setSearchDraft(event.currentTarget.value)}
              onBlur={() => setSearch(searchDraft)}
              onKeyDown={(event) => event.key === "Enter" && setSearch(searchDraft)}
            />
            <RadioButtonGroup
              size="sm"
              value={sort}
              options={sortOptions.map(({ label, value }) => ({
                label,
                value,
              }))}
              onChange={(value) =>
                locationService.partial({ sort: value === "total" ? undefined : value }, true)
              }
            />
          </div>
        }
        state={targets}
        empty="No targets match."
      >
        <DataTable
          key={sort}
          rows={targets.rows}
          rowKey={(row) => String(row.label)}
          sortedBy={sortOptions.find((option) => option.value === sort)?.column}
          columns={[
            targetColumn(),
            countColumn("Runs", "runs"),
            percentColumn("Cache hit rate", (row) => Number(row.hit_rate)),
            countColumn("Executed", "executed_runs"),
            durationColumn("Median", "p50_ms"),
            changeColumn(),
            durationColumn("p95", "p95_ms"),
            durationColumn("Total time", "total_ms"),
            criticalTimeColumn(),
            criticalShareColumn(),
            {
              ...countColumn("Failures", "failures"),
              className: (row) => (Number(row.failures) > 0 ? styles.bad : ""),
            },
          ]}
        />
      </Panel>
    </AppPage>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  actions: css({
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: theme.spacing(1),
  }),
  bad: css({ color: theme.colors.error.text }),
});
