import type { AppPluginMeta, PluginConfigPageProps } from "@grafana/data";
import { DataSourcePicker, getBackendSrv } from "@grafana/runtime";
import { Button, Field, Input } from "@grafana/ui";
import { useState } from "react";

import { pluginId } from "./links";
import { type AppSettings, defaultTracesTable } from "./sql/client";

export function ConfigPage({ plugin }: PluginConfigPageProps<AppPluginMeta<AppSettings>>) {
  const [datasourceUid, setDatasourceUid] = useState(plugin.meta.jsonData?.datasourceUid ?? "");
  const [tracesTable, setTracesTable] = useState(
    plugin.meta.jsonData?.tracesTable ?? defaultTracesTable,
  );

  const save = async () => {
    await getBackendSrv().post(`/api/plugins/${pluginId}/settings`, {
      enabled: true,
      pinned: true,
      jsonData: { datasourceUid, tracesTable },
    });
    window.location.reload();
  };

  return (
    <>
      <Field
        label="ClickHouse datasource"
        description="The datasource that holds the OpenTelemetry traces grog exports."
      >
        <DataSourcePicker
          type="grafana-clickhouse-datasource"
          current={datasourceUid || null}
          onChange={(datasource) => setDatasourceUid(datasource.uid)}
          noDefault
          width={40}
        />
      </Field>
      <Field
        label="Traces table"
        description="Table the OpenTelemetry ClickHouse exporter writes spans to, optionally prefixed with a database."
      >
        <Input
          value={tracesTable}
          onChange={(event) => setTracesTable(event.currentTarget.value)}
          width={40}
        />
      </Field>
      <Button onClick={save} disabled={!datasourceUid || !tracesTable}>
        Save
      </Button>
    </>
  );
}
