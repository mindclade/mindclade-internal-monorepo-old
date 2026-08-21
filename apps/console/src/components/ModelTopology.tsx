// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { TopologyGraph, type TopologyEdge, type TopologyNode } from "@mindclade/libs-ts-charts";

export function ModelTopology({ nodes, edges }: { nodes: readonly TopologyNode[]; edges: readonly TopologyEdge[] }): React.ReactNode {
  return <section className="panel"><div className="panel-heading"><div><h2>Execution topology</h2><p>Logical data and compute flow.</p></div></div><TopologyGraph label="Model execution topology" nodes={nodes} edges={edges} /></section>;
}
