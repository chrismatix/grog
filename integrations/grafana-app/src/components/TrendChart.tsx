import { createDataFrame, dateTime, FieldType, LoadingState, makeTimeRange } from "@grafana/data";
import { PanelRenderer } from "@grafana/runtime";
import { useMemo } from "react";

import type { Row } from "../sql/client";
import { useWidth } from "../useWidth";

export interface Series {
  key: string;
  label: string;
  unit: "ms" | "percentunit" | "short";
  color?: string;
}

export function TrendChart({
  rows,
  series,
  height = 220,
}: {
  rows: Row[];
  series: Series[];
  height?: number;
}) {
  const { reference, width } = useWidth();
  const data = useMemo(() => {
    const times = rows.map((row) => Number(row.bucket_ms));
    return {
      state: LoadingState.Done,
      timeRange: makeTimeRange(
        dateTime(times[0] ?? Date.now()),
        dateTime(times.at(-1) ?? Date.now()),
      ),
      series: [
        createDataFrame({
          fields: [
            { name: "Time", type: FieldType.time, values: times },
            ...series.map((entry) => ({
              name: entry.key,
              type: FieldType.number,
              values: rows.map((row) => Number(row[entry.key] ?? Number.NaN)),
              config: {
                unit: entry.unit,
                displayName: entry.label,
                ...(entry.color ? { color: { mode: "fixed", fixedColor: entry.color } } : {}),
              },
            })),
          ],
        }),
      ],
    };
  }, [rows, series]);

  return (
    <div ref={reference}>
      {width > 0 && (
        <PanelRenderer
          pluginId="timeseries"
          title=""
          width={width}
          height={height}
          data={data}
          options={{
            legend: {
              showLegend: true,
              displayMode: "list",
              placement: "bottom",
            },
            tooltip: { mode: "multi" },
          }}
          fieldConfig={{
            defaults: {
              custom: {
                lineWidth: 2,
                pointSize: 4,
                showPoints: rows.length < 40 ? "always" : "never",
                spanNulls: true,
              },
            },
            overrides: [],
          }}
        />
      )}
    </div>
  );
}
