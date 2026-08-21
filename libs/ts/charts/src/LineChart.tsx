// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement } from "react";

export interface LinePoint { x: number; y: number }

export function LineChart({ points, label, height = 180, stroke = "var(--mc-accent, #a6ffcb)" }: {
  points: readonly LinePoint[];
  label: string;
  height?: number;
  stroke?: string;
}): ReactElement {
  const width = 640;
  const values = points.length === 0 ? [{ x: 0, y: 0 }] : points;
  const xs = values.map((point) => point.x);
  const ys = values.map((point) => point.y);
  const minX = Math.min(...xs); const maxX = Math.max(...xs);
  const minY = Math.min(...ys); const maxY = Math.max(...ys);
  const px = (value: number): number => 24 + ((value - minX) / (maxX - minX || 1)) * (width - 48);
  const py = (value: number): number => 16 + (1 - (value - minY) / (maxY - minY || 1)) * (height - 38);
  const path = values.map((point, index) => `${index === 0 ? "M" : "L"}${px(point.x).toFixed(2)},${py(point.y).toFixed(2)}`).join(" ");
  return (
    <figure className="mc-chart" aria-label={label}>
      <svg role="img" aria-label={label} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
        {[0.25, 0.5, 0.75].map((step) => <line key={step} x1="24" x2={width - 24} y1={height * step} y2={height * step} stroke="var(--mc-line, #27303d)" vectorEffect="non-scaling-stroke" />)}
        <path d={path} fill="none" stroke={stroke} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
      </svg>
      <figcaption className="mc-visually-hidden">{label}; {points.length} samples from {minY} to {maxY}.</figcaption>
    </figure>
  );
}
