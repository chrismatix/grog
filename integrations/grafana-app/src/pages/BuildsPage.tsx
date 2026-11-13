import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { locationService } from "@grafana/runtime";
import { Button, Checkbox, LinkButton, RadioButtonGroup, TextLink, useStyles2 } from "@grafana/ui";
import { useState } from "react";

import { countColumn, durationColumn } from "../columns";
import { AppPage } from "../components/AppPage";
import { DataTable } from "../components/DataTable";
import { Panel } from "../components/Panel";
import type { Filters } from "../filters";
import {
  type BuildReference,
  buildUrl,
  compareUrl,
  formatCount,
  formatTime,
  githubRunUrl,
} from "../links";
import { buildsQuery, pageSize } from "../sql/builds";
import type { Row } from "../sql/client";
import { useSql } from "../useSql";

function referenceOf(build: Row): BuildReference {
  return {
    traceId: String(build.trace_id),
    startMs: Number(build.start_ms),
    durationMs: Number(build.duration_ms),
  };
}

export function BuildsPage({
  filters,
  status,
  limit,
}: {
  filters: Filters;
  status: string;
  limit: number;
}) {
  const styles = useStyles2(getStyles);
  const builds = useSql(buildsQuery(filters, status, limit));
  const [selected, setSelected] = useState<BuildReference[]>([]);
  const matching = Number(builds.rows[0]?.matching ?? 0);
  const toggle = (build: BuildReference) =>
    setSelected((previous) =>
      previous.some((entry) => entry.traceId === build.traceId)
        ? previous.filter((entry) => entry.traceId !== build.traceId)
        : [...previous, build].slice(-2),
    );
  // Older build first, so the comparison reads as "what changed since".
  const [base, head] = [...selected].sort((left, right) => left.startMs - right.startMs);

  return (
    <AppPage page="builds" filters={filters}>
      <Panel
        title={`Builds · ${formatCount(matching)}`}
        state={builds}
        empty="No builds match these filters."
        actions={
          <>
            <span className={styles.muted}>
              {selected.length === 2 ? "2 builds selected" : "Tick two builds to compare them"}
            </span>
            <LinkButton
              size="sm"
              icon="exchange-alt"
              variant="secondary"
              disabled={!head}
              href={head ? compareUrl(base, head) : undefined}
            >
              Compare
            </LinkButton>
            <RadioButtonGroup
              size="sm"
              value={status}
              options={[
                { label: "All", value: "" },
                { label: "With failures", value: "failed" },
                { label: "Reruns", value: "rerun" },
              ]}
              onChange={(value) =>
                locationService.partial({ status: value || undefined, limit: undefined }, true)
              }
            />
          </>
        }
      >
        <DataTable
          rows={builds.rows}
          rowKey={(build) => String(build.trace_id)}
          sortedBy="Started"
          columns={[
            {
              label: "",
              render: (build) => (
                <Checkbox
                  aria-label="Select for comparison"
                  value={selected.some((entry) => entry.traceId === String(build.trace_id))}
                  onChange={() => toggle(referenceOf(build))}
                />
              ),
            },
            {
              label: "Started",
              sortValue: (build) => Number(build.start_ms),
              render: (build) => (
                <TextLink
                  href={buildUrl(
                    String(build.trace_id),
                    Number(build.start_ms),
                    Number(build.duration_ms),
                  )}
                  inline={false}
                >
                  {formatTime(Number(build.start_ms))}
                </TextLink>
              ),
            },
            {
              label: "Workflow / job",
              render: (build) =>
                build.workflow ? (
                  `${build.workflow} / ${build.job}`
                ) : (
                  <span className={styles.muted}>{String(build.command)}</span>
                ),
            },
            {
              label: "Branch",
              className: () => styles.truncate,
              render: (build) => <span title={String(build.branch)}>{String(build.branch)}</span>,
            },
            {
              label: "Arch",
              className: () => styles.muted,
              render: (build) => String(build.arch),
            },
            durationColumn("Duration", "duration_ms"),
            durationColumn("Critical path", "critical_path_ms"),
            {
              label: "Executed / targets",
              numeric: true,
              sortValue: (build) => Number(build.executed),
              render: (build) => `${build.executed} / ${build.targets}`,
            },
            {
              ...countColumn("Failures", "failures"),
              className: (build) => (Number(build.failures) > 0 ? styles.bad : ""),
            },
            {
              label: "Run",
              render: (build) =>
                build.run_id ? (
                  <TextLink
                    href={githubRunUrl(
                      String(build.repository),
                      String(build.run_id),
                      String(build.run_attempt),
                    )}
                    external
                  >
                    {`#${build.run_id}${Number(build.run_attempt) > 1 ? ` · attempt ${build.run_attempt}` : ""}`}
                  </TextLink>
                ) : null,
            },
          ]}
        />
        {matching > builds.rows.length && (
          <div className={styles.footer}>
            <span className={styles.muted}>
              Showing the latest {formatCount(builds.rows.length)} of {formatCount(matching)}
            </span>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => locationService.partial({ limit: limit + pageSize }, true)}
            >
              Show {formatCount(Math.min(pageSize, matching - limit))} more
            </Button>
          </div>
        )}
      </Panel>
    </AppPage>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  muted: css({
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
  }),
  truncate: css({
    maxWidth: 320,
    overflow: "hidden",
    textOverflow: "ellipsis",
  }),
  bad: css({ color: theme.colors.error.text }),
  footer: css({
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    gap: theme.spacing(2),
    marginTop: theme.spacing(2),
  }),
});
