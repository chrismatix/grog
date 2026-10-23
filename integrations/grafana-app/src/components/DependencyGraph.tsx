import dagre from "@dagrejs/dagre";
import { css } from "@emotion/css";
import type { GrafanaTheme2 } from "@grafana/data";
import { locationService } from "@grafana/runtime";
import { RadioButtonGroup, useStyles2, useTheme2 } from "@grafana/ui";
import { useMemo, useState } from "react";

import { formatDuration, targetUrl } from "../links";

export interface TargetSpan {
  label: string;
  startMs: number;
  durationMs: number;
  commandMs: number;
  cacheResult: string;
  failed: boolean;
  dependencies: string[];
  phases: Record<string, number>;
}

const nodeWidth = 280;
const nodeHeight = 44;

// Edges skip hidden nodes, so a visible target links to its nearest visible ancestors.
function visibleEdges(
  spans: Map<string, TargetSpan>,
  visible: Set<string>,
): Array<[string, string]> {
  const nearestVisible = new Map<string, Set<string>>();
  const resolve = (label: string): Set<string> => {
    const cached = nearestVisible.get(label);
    if (cached) {
      return cached;
    }
    const ancestors = new Set<string>();
    nearestVisible.set(label, ancestors);
    for (const dependency of spans.get(label)?.dependencies ?? []) {
      if (!spans.has(dependency)) {
        continue;
      }
      if (visible.has(dependency)) {
        ancestors.add(dependency);
      } else {
        resolve(dependency).forEach((ancestor) => ancestors.add(ancestor));
      }
    }
    return ancestors;
  };
  const edges: Array<[string, string]> = [];
  for (const label of visible) {
    resolve(label).forEach((dependency) => edges.push([dependency, label]));
  }
  return edges;
}

export function DependencyGraph({
  spans,
  criticalPath,
}: {
  spans: TargetSpan[];
  criticalPath: string[];
}) {
  const styles = useStyles2(getStyles);
  const theme = useTheme2();
  const [showCacheHits, setShowCacheHits] = useState(false);
  const [zoom, setZoom] = useState(1);
  const critical = useMemo(() => new Set(criticalPath), [criticalPath]);

  const layout = useMemo(() => {
    const spansByLabel = new Map(spans.map((span) => [span.label, span]));
    const visible = new Set(
      spans
        .filter(
          (span) =>
            showCacheHits ||
            span.cacheResult !== "CACHE_HIT" ||
            span.failed ||
            critical.has(span.label),
        )
        .map((span) => span.label),
    );
    const graph = new dagre.graphlib.Graph();
    graph.setGraph({
      rankdir: "LR",
      nodesep: 12,
      ranksep: 60,
      marginx: 12,
      marginy: 12,
    });
    graph.setDefaultEdgeLabel(() => ({}));
    visible.forEach((label) => graph.setNode(label, { width: nodeWidth, height: nodeHeight }));
    visibleEdges(spansByLabel, visible).forEach(([from, to]) => graph.setEdge(from, to));
    dagre.layout(graph);
    return { graph, spansByLabel };
  }, [spans, showCacheHits, critical]);

  const { graph, spansByLabel } = layout;
  const size = graph.graph();
  const colorFor = (span: TargetSpan) => {
    if (span.failed) {
      return theme.visualization.getColorByName("red");
    }
    if (span.cacheResult === "CACHE_HIT") {
      return theme.colors.background.secondary;
    }
    return theme.visualization.getColorByName("blue");
  };
  const isOnCriticalPath = (from: string, to: string) =>
    critical.has(from) &&
    critical.has(to) &&
    criticalPath.indexOf(to) === criticalPath.indexOf(from) + 1;

  return (
    <div>
      <div className={styles.controls}>
        <RadioButtonGroup
          size="sm"
          value={showCacheHits}
          options={[
            { label: "Executed + critical path", value: false },
            { label: "All targets", value: true },
          ]}
          onChange={setShowCacheHits}
        />
        <RadioButtonGroup
          size="sm"
          value={zoom}
          options={[0.5, 0.75, 1].map((value) => ({
            label: `${value * 100}%`,
            value,
          }))}
          onChange={setZoom}
        />
        <span className={styles.legend}>
          <span
            className={styles.swatch}
            style={{ background: theme.visualization.getColorByName("blue") }}
          />{" "}
          executed
          <span
            className={styles.swatch}
            style={{ background: theme.colors.background.secondary }}
          />{" "}
          cache hit
          <span
            className={styles.swatch}
            style={{ background: theme.visualization.getColorByName("red") }}
          />{" "}
          failed
          <span
            className={styles.swatch}
            style={{
              border: `3px solid ${theme.visualization.getColorByName("orange")}`,
            }}
          />{" "}
          critical path
        </span>
      </div>
      <div className={styles.canvas}>
        <svg
          width={(size.width ?? 0) * zoom}
          height={(size.height ?? 0) * zoom}
          viewBox={`0 0 ${size.width} ${size.height}`}
        >
          <defs>
            <marker
              id="grog-arrow"
              viewBox="0 0 10 10"
              refX="10"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto"
            >
              <path d="M0,0 L10,5 L0,10 z" fill={theme.colors.text.disabled} />
            </marker>
          </defs>
          {graph.edges().map((edge) => {
            const points = graph.edge(edge).points ?? [];
            const highlighted = isOnCriticalPath(edge.v, edge.w);
            return (
              <polyline
                key={`${edge.v}->${edge.w}`}
                points={points
                  .map((point: { x: number; y: number }) => `${point.x},${point.y}`)
                  .join(" ")}
                fill="none"
                stroke={
                  highlighted
                    ? theme.visualization.getColorByName("orange")
                    : theme.colors.border.medium
                }
                strokeWidth={highlighted ? 3 : 1}
                markerEnd="url(#grog-arrow)"
              />
            );
          })}
          {graph.nodes().map((label) => {
            const node = graph.node(label);
            const span = spansByLabel.get(label);
            if (!node || !span) {
              return null;
            }
            const hit = span.cacheResult === "CACHE_HIT" && !span.failed;
            return (
              <g
                key={label}
                transform={`translate(${node.x - nodeWidth / 2},${node.y - nodeHeight / 2})`}
                className={styles.node}
                onClick={() => locationService.push(targetUrl(label))}
              >
                <title>{`${label}\n${span.cacheResult}${span.failed ? " · FAILED" : ""}\ncommand ${formatDuration(span.commandMs)} · total ${formatDuration(span.durationMs)}`}</title>
                <rect
                  width={nodeWidth}
                  height={nodeHeight}
                  rx={4}
                  fill={colorFor(span)}
                  stroke={
                    critical.has(label)
                      ? theme.visualization.getColorByName("orange")
                      : theme.colors.border.weak
                  }
                  strokeWidth={critical.has(label) ? 3 : 1}
                />
                <text
                  x={8}
                  y={18}
                  className={styles.label}
                  fill={hit ? theme.colors.text.primary : "#fff"}
                >
                  {label.length > 40 ? `…${label.slice(-39)}` : label}
                </text>
                <text
                  x={8}
                  y={35}
                  className={styles.detail}
                  fill={hit ? theme.colors.text.secondary : "#fff"}
                >
                  {hit ? "cache hit" : `command ${formatDuration(span.commandMs)}`}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  controls: css({
    display: "flex",
    flexWrap: "wrap",
    gap: theme.spacing(2),
    alignItems: "center",
    marginBottom: theme.spacing(1),
  }),
  legend: css({
    display: "flex",
    alignItems: "center",
    gap: theme.spacing(0.75),
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
  }),
  swatch: css({
    display: "inline-block",
    width: 12,
    height: 12,
    borderRadius: 2,
    marginLeft: theme.spacing(1),
  }),
  canvas: css({
    overflow: "auto",
    maxHeight: "70vh",
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    background: theme.colors.background.canvas,
  }),
  node: css({
    cursor: "pointer",
    "&:hover rect": { filter: "brightness(1.15)" },
  }),
  label: css({
    fontSize: 12,
    fontFamily: theme.typography.fontFamilyMonospace,
  }),
  detail: css({ fontSize: 11 }),
});
