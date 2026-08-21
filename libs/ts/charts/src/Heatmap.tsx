// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement } from "react";

export function Heatmap({ values, label, rowLabels = [], columnLabels = [] }: {
  values: readonly (readonly number[])[];
  label: string;
  rowLabels?: readonly string[];
  columnLabels?: readonly string[];
}): ReactElement {
  const rows = values.length;
  const columns = Math.max(0, ...values.map((row) => row.length));
  const flat = values.flat();
  const min = Math.min(...flat, 0); const max = Math.max(...flat, 1);
  return (
    <figure className="mc-chart" aria-label={label}>
      <svg role="img" aria-label={label} viewBox={`0 0 ${Math.max(columns, 1) * 28} ${Math.max(rows, 1) * 28}`}>
        {values.flatMap((row, rowIndex) => row.map((value, columnIndex) => {
          const intensity = (value - min) / (max - min || 1);
          const description = `${rowLabels[rowIndex] ?? `row ${rowIndex + 1}`}, ${columnLabels[columnIndex] ?? `column ${columnIndex + 1}`}: ${value}`;
          return <rect key={`${rowIndex}-${columnIndex}`} x={columnIndex * 28 + 1} y={rowIndex * 28 + 1} width="26" height="26" rx="4" fill="var(--mc-accent, #a6ffcb)" opacity={.08 + intensity * .86}><title>{description}</title></rect>;
        }))}
      </svg>
      <figcaption className="mc-visually-hidden">{label}; {rows} rows by {columns} columns.</figcaption>
    </figure>
  );
}
