import {
  countColumn,
  flakyInputsColumn,
  lastFailureColumn,
  percentColumn,
  targetColumn,
} from "../columns";
import { AppPage } from "../components/AppPage";
import { DataTable } from "../components/DataTable";
import { Panel, PanelGrid } from "../components/Panel";
import { StatGrid } from "../components/StatGrid";
import { TargetLabel } from "../components/TargetLabel";
import { TrendChart } from "../components/TrendChart";
import { type Filters, rangeLabel } from "../filters";
import { formatCount, formatPercent } from "../links";
import { kpiQuery, rerunsQuery, trendQuery } from "../sql/builds";
import { failingTargetsQuery, flakyTargetsQuery } from "../sql/targets";
import { useSql } from "../useSql";

export function ReliabilityPage({ filters }: { filters: Filters }) {
  const kpis = useSql(kpiQuery(filters));
  const trend = useSql(trendQuery(filters));
  const flaky = useSql(flakyTargetsQuery(filters, 50));
  const failing = useSql(failingTargetsQuery(filters, 50));
  const reruns = useSql(rerunsQuery(filters));

  const current = kpis.rows.find((row) => Number(row.is_current) === 1) ?? {};
  const previous = kpis.rows.find((row) => Number(row.is_current) === 0);
  const compare = (key: string) =>
    previous
      ? {
          previous: Number(previous[key]),
          kind: "points" as const,
          higherIsBetter: false,
        }
      : undefined;

  return (
    <AppPage page="reliability" filters={filters}>
      <Panel title="Reliability" state={kpis}>
        <StatGrid
          previousLabel={`previous ${rangeLabel(filters.range)}`}
          stats={[
            {
              label: "Builds with failures",
              value: formatPercent(Number(current.failure_rate)),
              current: Number(current.failure_rate),
              comparison: compare("failure_rate"),
            },
            {
              label: "Rerun builds",
              value: formatPercent(Number(current.rerun_rate)),
              current: Number(current.rerun_rate),
              comparison: compare("rerun_rate"),
              hint: "Builds from a GitHub run attempt above 1",
            },
            {
              label: "Flaky targets",
              value: formatCount(Number(flaky.rows[0]?.all_flaky_targets ?? 0)),
              hint: "Targets that both failed and passed with identical inputs",
            },
            {
              label: "Failing targets",
              value: formatCount(Number(failing.rows[0]?.all_failing_targets ?? 0)),
            },
          ]}
        />
      </Panel>

      <Panel title="Failures and reruns" state={trend}>
        <TrendChart
          height={200}
          rows={trend.rows}
          series={[
            {
              key: "failure_rate",
              label: "Builds with failures",
              unit: "percentunit",
              color: "red",
            },
            {
              key: "rerun_rate",
              label: "Rerun builds",
              unit: "percentunit",
              color: "orange",
            },
          ]}
        />
      </Panel>

      <Panel
        title="Flaky targets"
        help="Same change hash, different outcome: the failure was not caused by a code change. Quarantine or fix these first, since each one fails unrelated PRs."
        state={flaky}
        empty="No target both failed and passed with the same inputs."
      >
        <DataTable
          rows={flaky.rows}
          rowKey={(row) => String(row.label)}
          sortedBy="Flaky inputs"
          columns={[
            {
              label: "Target",
              render: (row) => (
                <TargetLabel
                  label={String(row.label)}
                  tags={Number(row.test) === 1 ? ["test"] : []}
                />
              ),
            },
            flakyInputsColumn(),
            countColumn("Failures", "total_failures"),
            countColumn("Passes", "total_passes"),
            percentColumn(
              "Failure rate",
              (row) =>
                Number(row.total_failures) /
                Math.max(Number(row.total_failures) + Number(row.total_passes), 1),
              "Failures among runs of the flaky inputs",
            ),
            lastFailureColumn(),
          ]}
        />
      </Panel>

      <PanelGrid minimumWidth={560}>
        <Panel title="Most failing targets" state={failing} empty="No target failed in this range.">
          <DataTable
            rows={failing.rows}
            rowKey={(row) => String(row.label)}
            sortedBy="Failures"
            columns={[
              targetColumn(),
              countColumn("Failures", "failures"),
              percentColumn(
                "Failure rate",
                (row) => Number(row.failure_rate),
                "Failures among executed runs",
              ),
              countColumn("Failing inputs", "failing_hashes", "Distinct change hashes that failed"),
              lastFailureColumn(),
            ]}
          />
        </Panel>
        <Panel
          title="Reruns by workflow"
          help="Share of GitHub runs that needed at least one manual re-run"
          state={reruns}
        >
          <DataTable
            rows={reruns.rows}
            rowKey={(row) => `${row.workflow}/${row.job}`}
            sortedBy="Rerun runs"
            columns={[
              {
                label: "Workflow / job",
                render: (row) => `${row.workflow} / ${row.job}`,
              },
              countColumn("Runs", "runs"),
              countColumn("Rerun runs", "rerun_runs"),
              percentColumn("Rerun rate", (row) => Number(row.rerun_rate)),
            ]}
          />
        </Panel>
      </PanelGrid>
    </AppPage>
  );
}
