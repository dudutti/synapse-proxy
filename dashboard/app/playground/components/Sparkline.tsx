"use client";

import { useMemo } from "react";

// Tiny inline sparkline: renders an SVG polyline of normalised values.
// No external chart lib needed (recharts/visx would add 200KB+).
export function Sparkline({
  values,
  color = "#34d399",
  height = 24,
  width = 80,
  fillOpacity = 0.15,
}: {
  values: number[];
  color?: string;
  height?: number;
  width?: number;
  fillOpacity?: number;
}) {
  const { line, fill } = useMemo(() => {
    if (!values || values.length === 0) return { line: "", fill: "" };
    const max = Math.max(...values, 1);
    const min = Math.min(...values, 0);
    const range = max - min || 1;
    const step = values.length > 1 ? width / (values.length - 1) : width;
    const points = values.map((v, i) => {
      const x = i * step;
      const y = height - ((v - min) / range) * height;
      return [x, y] as const;
    });
    const linePath = points
      .map(([x, y], i) => (i === 0 ? `M ${x},${y}` : `L ${x},${y}`))
      .join(" ");
    const fillPath = `${linePath} L ${width},${height} L 0,${height} Z`;
    return { line: linePath, fill: fillPath };
  }, [values, height, width]);

  if (!line) {
    return (
      <div
        className="inline-block bg-white/5 rounded"
        style={{ width, height }}
      />
    );
  }

  return (
    <svg width={width} height={height} className="inline-block">
      <path d={fill} fill={color} fillOpacity={fillOpacity} />
      <path d={line} stroke={color} strokeWidth={1.5} fill="none" />
    </svg>
  );
}
