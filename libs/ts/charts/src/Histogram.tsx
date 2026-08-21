// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement } from "react";

export function histogram(values: readonly number[], binCount = 12): number[] {
  const bins = Array.from({ length: Math.max(1, binCount) }, () => 0);
  if (values.length === 0) return bins;
  const min = Math.min(...values); const max = Math.max(...values);
  for (const value of values) {
    const index = Math.min(bins.length - 1, Math.floor(((value - min) / (max - min || 1)) * bins.length));
    bins[index] = (bins[index] ?? 0) + 1;
  }
  return bins;
}

export function Histogram({ values, label, bins = 12 }: { values: readonly number[]; label: string; bins?: number }): ReactElement {
  const counts = histogram(values, bins);
  const max = Math.max(...counts, 1);
  return (
    <figure className="mc-chart" aria-label={label}>
      <svg role="img" aria-label={label} viewBox="0 0 640 180" preserveAspectRatio="none">
        {counts.map((count, index) => {
          const width = 600 / counts.length;
          const height = (count / max) * 140;
          return <rect key={index} x={20 + index * width + 1} y={160 - height} width={Math.max(width - 2, 1)} height={height} rx="2" fill="var(--mc-info, #7bbcff)" opacity={.45 + count / max * .45} />;
        })}
      </svg>
      <figcaption className="mc-visually-hidden">{label}; {values.length} observations across {counts.length} bins.</figcaption>
    </figure>
  );
}
