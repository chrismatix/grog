import { css } from "@emotion/css";
import type { GrafanaTheme2, IconName } from "@grafana/data";
import { locationService, PluginPage } from "@grafana/runtime";
import {
  Button,
  Field,
  Input,
  RadioButtonGroup,
  Select,
  Tab,
  TabsBar,
  useStyles2,
} from "@grafana/ui";
import { type ReactNode, useEffect, useState } from "react";

import { type Filters, rangeOptions } from "../filters";
import { appUrl } from "../links";
import { filterValuesQuery, tagValuesQuery } from "../sql/builds";
import { useSql } from "../useSql";

export const sections: Array<{ page: string; label: string; icon: IconName }> = [
  { page: "", label: "Overview", icon: "apps" },
  { page: "regressions", label: "Regressions", icon: "arrow-up" },
  { page: "targets", label: "Targets", icon: "cube" },
  { page: "cache", label: "Cache", icon: "database" },
  { page: "reliability", label: "Reliability", icon: "bug" },
  { page: "builds", label: "Builds", icon: "list-ul" },
  { page: "jobs", label: "CI jobs", icon: "github" },
];

export function AppPage({
  page,
  filters,
  children,
}: {
  page: string;
  filters: Filters;
  children: ReactNode;
}) {
  const styles = useStyles2(getStyles);
  const section = sections.find((entry) => entry.page === page);
  return (
    <PluginPage pageNav={{ text: section?.label ?? "Grog CI" }}>
      <TabsBar className={styles.tabs}>
        {sections.map((entry) => (
          <Tab
            key={entry.page}
            label={entry.label}
            icon={entry.icon}
            href={appUrl(entry.page)}
            active={entry.page === page}
          />
        ))}
      </TabsBar>
      <FilterBar filters={filters} />
      {children}
    </PluginPage>
  );
}

const anyOption = { label: "Any", value: "" };

function toOptions(values: unknown) {
  return [
    anyOption,
    ...((values as string[] | undefined) ?? [])
      .filter(Boolean)
      .sort()
      .map((value) => ({ label: value, value })),
  ];
}

export function FilterBar({ filters }: { filters: Filters }) {
  const styles = useStyles2(getStyles);
  const [branchDraft, setBranchDraft] = useState(filters.branch);
  useEffect(() => setBranchDraft(filters.branch), [filters.branch]);
  const values = useSql(filterValuesQuery(filters.range));
  const tags = useSql(tagValuesQuery(filters.range));
  const setFilter = (key: keyof Filters, value: string) =>
    locationService.partial({ [key]: value || undefined }, true);
  const tagOptions = [
    anyOption,
    ...tags.rows.flatMap((row) =>
      ((row.tag_values as string[]) ?? []).filter(Boolean).map((value) => ({
        label: `${String(row.tag).replace("github.", "")}: ${value}`,
        value: `${row.tag}=${value}`,
      })),
    ),
  ];
  const active = filters.workflow || filters.job || filters.branch || filters.arch || filters.tag;

  return (
    <div className={styles.filters}>
      <Field label="Range" className={styles.field}>
        <RadioButtonGroup
          value={filters.range}
          options={rangeOptions.map((value) => ({
            label: value.replace(" DAY", "d").replace(/^1d$/, "24h"),
            value,
          }))}
          onChange={(value) => setFilter("range", value)}
        />
      </Field>
      <Field label="Workflow" className={styles.field}>
        <Select
          width={22}
          value={filters.workflow}
          options={toOptions(values.rows[0]?.workflows)}
          onChange={(option) => setFilter("workflow", option.value ?? "")}
        />
      </Field>
      <Field label="Job" className={styles.field}>
        <Select
          width={16}
          value={filters.job}
          options={toOptions(values.rows[0]?.jobs)}
          onChange={(option) => setFilter("job", option.value ?? "")}
        />
      </Field>
      <Field label="Branch" className={styles.field}>
        <Input
          width={32}
          placeholder="any branch"
          addonBefore={
            <RadioButtonGroup
              value={filters.branchMatch}
              options={[
                { label: "contains", value: "contains" },
                { label: "equals", value: "equals" },
              ]}
              onChange={(value) => setFilter("branchMatch", value === "contains" ? "" : value)}
            />
          }
          value={branchDraft}
          onChange={(event) => setBranchDraft(event.currentTarget.value)}
          onBlur={() => setFilter("branch", branchDraft)}
          onKeyDown={(event) => event.key === "Enter" && setFilter("branch", branchDraft)}
        />
      </Field>
      <Field label="Arch" className={styles.field}>
        <RadioButtonGroup
          value={filters.arch}
          options={[
            { label: "Any", value: "" },
            ...((values.rows[0]?.arches as string[] | undefined) ?? [])
              .filter(Boolean)
              .sort()
              .map((value) => ({ label: value, value })),
          ]}
          onChange={(value) => setFilter("arch", value)}
        />
      </Field>
      <Field label="Tag" className={styles.field}>
        <Select
          width={24}
          value={filters.tag}
          options={tagOptions}
          onChange={(option) => setFilter("tag", option.value ?? "")}
        />
      </Field>
      {active && (
        <Button
          variant="secondary"
          fill="text"
          icon="times"
          className={styles.clear}
          onClick={() =>
            locationService.partial(
              {
                workflow: undefined,
                job: undefined,
                branch: undefined,
                branchMatch: undefined,
                arch: undefined,
                tag: undefined,
              },
              true,
            )
          }
        >
          Clear filters
        </Button>
      )}
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  tabs: css({ marginBottom: theme.spacing(2) }),
  filters: css({
    display: "flex",
    flexWrap: "wrap",
    gap: theme.spacing(1.5),
    alignItems: "flex-end",
    marginBottom: theme.spacing(2),
  }),
  field: css({ marginBottom: 0 }),
  clear: css({ marginBottom: theme.spacing(0.25) }),
});
