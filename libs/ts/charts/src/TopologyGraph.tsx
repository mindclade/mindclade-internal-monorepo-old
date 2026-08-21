// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement } from "react";

export interface TopologyNode { id: string; label: string; x: number; y: number; tone?: "neutral" | "active" | "warning" }
export interface TopologyEdge { from: string; to: string; label?: string }

export function TopologyGraph({ nodes, edges, label }: { nodes: readonly TopologyNode[]; edges: readonly TopologyEdge[]; label: string }): ReactElement {
  const byId = new Map(nodes.map((node) => [node.id, node]));
  return (
    <figure className="mc-chart" aria-label={label}>
      <svg role="img" aria-label={label} viewBox="0 0 640 260">
        {edges.map((edge) => {
          const from = byId.get(edge.from); const to = byId.get(edge.to);
          if (from === undefined || to === undefined) return null;
          return <g key={`${edge.from}-${edge.to}`}><line x1={from.x} y1={from.y} x2={to.x} y2={to.y} stroke="var(--mc-line, #27303d)" strokeWidth="2" /><circle cx={(from.x + to.x) / 2} cy={(from.y + to.y) / 2} r="2.5" fill="var(--mc-accent, #a6ffcb)" /></g>;
        })}
        {nodes.map((node) => <g key={node.id} transform={`translate(${node.x} ${node.y})`}><circle r="15" fill={node.tone === "active" ? "var(--mc-accent, #a6ffcb)" : "var(--mc-panel-strong, #171e29)"} stroke={node.tone === "warning" ? "var(--mc-warning, #ffd37a)" : "var(--mc-line, #27303d)"} strokeWidth="2" /><text x="0" y="31" textAnchor="middle" fill="var(--mc-text-muted, #96a2b2)" fontSize="11">{node.label}</text></g>)}
      </svg>
      <figcaption className="mc-visually-hidden">{label}; {nodes.length} nodes and {edges.length} connections.</figcaption>
    </figure>
  );
}
