import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { locationService } from "@grafana/runtime";
import { useStyles2, useTheme2 } from "@grafana/ui";
import { useMemo } from "react";

import { buildUrl, formatDuration, formatTime } from "../links";
import { useWidth } from "../useWidth";

export interface ScatterRun {
  traceId: string;
  startMs: number;
  commandMs: number;
  arch: string;
  buildStartMs: number;
  buildDurationMs: number;
  job: string;
  branch: string;
}

const height = 260;
const dayMs = 86_400_000;
const margin = { top: 10, right: 16, bottom: 28, left: 64 };
const archColors: Record<string, string> = { amd64: "green", arm64: "yellow" };

export function RunScatter({ runs }: { runs: ScatterRun[] }) {
  const styles = useStyles2(getStyles);
  const theme = useTheme2();
  const { reference, width } = useWidth();

  const times = runs.map((run) => run.startMs);
  const minimumTime = Math.min(...times);
  const timeSpan = Math.max(Math.max(...times) - minimumTime, 1);
  const maximumValue = Math.max(...runs.map((run) => run.commandMs), 1);
  const plotWidth = Math.max(width - margin.left - margin.right, 1);
  const plotHeight = height - margin.top - margin.bottom;
  const xOf = (time: number) => margin.left + ((time - minimumTime) / timeSpan) * plotWidth;
  const yOf = (value: number) => margin.top + plotHeight - (value / maximumValue) * plotHeight;
  const ticks = [0, 0.25, 0.5, 0.75, 1];
  const dailyMedians = useMemo(() => {
    const byDay = new Map<number, number[]>();
    for (const run of runs) {
      const day = Math.floor(run.startMs / dayMs) * dayMs + dayMs / 2;
      byDay.set(day, [...(byDay.get(day) ?? []), run.commandMs]);
    }
    return [...byDay.entries()]
      .sort(([left], [right]) => left - right)
      .map(([day, values]) => {
        const sorted = [...values].sort((left, right) => left - right);
        return { day, median: sorted[Math.floor(sorted.length / 2)] };
      });
  }, [runs]);

  return (
    <div ref={reference}>
      {width > 0 && runs.length > 0 && (
        <svg width={width} height={height}>
          {ticks.map((tick) => (
            <g key={`y${tick}`}>
              <line
                x1={margin.left}
                x2={width - margin.right}
                y1={yOf(tick * maximumValue)}
                y2={yOf(tick * maximumValue)}
                stroke={theme.colors.border.weak}
              />
              <text
                x={margin.left - 8}
                y={yOf(tick * maximumValue) + 4}
                textAnchor="end"
                className={styles.axis}
              >
                {formatDuration(tick * maximumValue)}
              </text>
              <text
                x={xOf(minimumTime + tick * timeSpan)}
                y={height - 8}
                textAnchor={tick === 0 ? "start" : tick === 1 ? "end" : "middle"}
                className={styles.axis}
              >
                {formatTime(minimumTime + tick * timeSpan)}
              </text>
            </g>
          ))}
          {runs.map((run) => (
            <circle
              key={`${run.traceId}-${run.startMs}`}
              cx={xOf(run.startMs)}
              cy={yOf(run.commandMs)}
              r={3.5}
              fill={theme.visualization.getColorByName(archColors[run.arch] ?? "blue")}
              className={styles.point}
              onClick={() =>
                locationService.push(buildUrl(run.traceId, run.buildStartMs, run.buildDurationMs))
              }
            >
              <title>
                {`${formatTime(run.startMs)} · ${formatDuration(run.commandMs)} · ${run.arch}\n${run.job} · ${run.branch}\nClick to open the build`}
              </title>
            </circle>
          ))}
          <polyline
            fill="none"
            stroke={theme.visualization.getColorByName("orange")}
            strokeWidth={2}
            pointerEvents="none"
            points={dailyMedians.map(({ day, median }) => `${xOf(day)},${yOf(median)}`).join(" ")}
          />
        </svg>
      )}
      <div className={styles.legend}>
        {Object.entries(archColors).map(([arch, color]) => (
          <span key={arch}>
            <span
              className={styles.swatch}
              style={{ background: theme.visualization.getColorByName(color) }}
            />{" "}
            {arch}
          </span>
        ))}
        <span>
          <span className={styles.medianSwatch} /> daily median · click a point to open its build
        </span>
      </div>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  axis: css({ fill: theme.colors.text.secondary, fontSize: 11 }),
  point: css({
    cursor: "pointer",
    opacity: 0.75,
    "&:hover": {
      opacity: 1,
      r: 6,
      stroke: theme.colors.text.primary,
      strokeWidth: 1.5,
    },
  }),
  legend: css({
    display: "flex",
    gap: theme.spacing(1.5),
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
  }),
  medianSwatch: css({
    display: "inline-block",
    width: 14,
    height: 2,
    verticalAlign: "middle",
    background: theme.visualization.getColorByName("orange"),
  }),
  swatch: css({
    display: "inline-block",
    width: 10,
    height: 10,
    borderRadius: "50%",
  }),
});
