import { getBackendSrv, toDataQueryResponse } from "@grafana/runtime";

export interface AppSettings {
  datasourceUid?: string;
  tracesTable?: string;
}

export const defaultTracesTable = "otel_traces";

// Set from the plugin's jsonData before any page renders.
export const settings = { datasourceUid: "", tracesTable: defaultTracesTable };

export type Row = Record<string, unknown>;

export async function runSql(rawSql: string): Promise<Row[]> {
  const results = await getBackendSrv().post("/api/ds/query", {
    queries: [
      {
        refId: "A",
        datasource: { uid: settings.datasourceUid },
        rawSql,
        format: 1,
        queryType: "table",
        editorType: "sql",
      },
    ],
  });
  const { data, error } = toDataQueryResponse({ data: results });
  if (error) {
    throw new Error(error.message);
  }
  const frame = data[0];
  if (!frame) {
    return [];
  }
  const rows: Row[] = [];
  for (let index = 0; index < frame.length; index++) {
    const row: Row = {};
    for (const field of frame.fields) {
      row[field.name] = field.values[index];
    }
    rows.push(row);
  }
  return rows;
}

export function quote(value: string): string {
  return `'${value.replaceAll("\\", "\\\\").replaceAll("'", "\\'")}'`;
}
