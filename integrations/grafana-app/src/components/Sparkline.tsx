import { useTheme2 } from "@grafana/ui";

const width = 120;
const height = 28;

export function Sparkline({
  values,
  markIndex,
}: {
  values: number[];
  // Draws a dashed marker before this point, e.g. a detected step change.
  markIndex?: number;
}) {
  const theme = useTheme2();
  if (values.length < 2) {
    return null;
  }
  const maximum = Math.max(...values, 1);
  const xOf = (index: number) => (index / (values.length - 1)) * (width - 2) + 1;
  const yOf = (value: number) => height - 2 - (value / maximum) * (height - 4);
  return (
    <svg width={width} height={height} aria-hidden>
      {markIndex !== undefined && (
        <line
          x1={xOf(markIndex - 0.5)}
          x2={xOf(markIndex - 0.5)}
          y1={0}
          y2={height}
          stroke={theme.colors.text.disabled}
          strokeDasharray="2 2"
        />
      )}
      <polyline
        fill="none"
        stroke={theme.visualization.getColorByName("blue")}
        strokeWidth={1.5}
        points={values.map((value, index) => `${xOf(index)},${yOf(value)}`).join(" ")}
      />
    </svg>
  );
}

// The split that best separates the series into a "before" and "after" level.
export function findStepChange(values: number[]): number | undefined {
  let best: { index: number; score: number } | undefined;
  for (let index = 2; index <= values.length - 2; index++) {
    const before = values.slice(0, index);
    const after = values.slice(index);
    const mean = (list: number[]) => list.reduce((sum, value) => sum + value, 0) / list.length;
    const score = Math.abs(mean(after) - mean(before));
    if (!best || score > best.score) {
      best = { index, score };
    }
  }
  return best?.index;
}
