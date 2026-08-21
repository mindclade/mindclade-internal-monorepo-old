// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { TopologyGraph } from "@mindclade/libs-ts-charts";
import { Button, Metric, StatusBadge } from "@mindclade/libs-ts-design-system";
import Link from "next/link";

const nodes = [
  { id: "data", label: "Data", x: 82, y: 132 },
  { id: "train", label: "Training", x: 238, y: 68, tone: "active" as const },
  { id: "eval", label: "Evaluation", x: 398, y: 132 },
  { id: "serve", label: "Serving", x: 556, y: 68 },
  { id: "artifacts", label: "Artifacts", x: 318, y: 224 },
];
const edges = [{ from: "data", to: "train" }, { from: "train", to: "eval" }, { from: "eval", to: "serve" }, { from: "train", to: "artifacts" }, { from: "eval", to: "artifacts" }];

export default function Page(): React.ReactNode {
  return <div className="page-stack dashboard"><header className="command-hero"><div><span className="eyebrow">AI systems command</span><h1>See the whole machine.<br /><em>Move with evidence.</em></h1><p>One operational surface for durable runs, qualified models, immutable data, and the proof behind every promotion.</p><div className="hero-actions"><Button tone="primary" disabled title="Run creation requires a connected API session">New run</Button><Link className="text-link" href="/evaluations">Review evaluations →</Link></div></div><div className="system-radar" aria-hidden="true"><span /><span /><span /><i /></div></header><section className="metric-strip" aria-label="Workspace signal"><Metric label="Active runs" value="—" detail="Connect an environment" /><Metric label="Qualified models" value="—" detail="Canonical registry" /><Metric label="Evaluation gates" value="—" detail="Independent evidence" /><Metric label="Artifact trust" value="—" detail="Content addressed" /></section><div className="dashboard-grid"><section className="panel topology-panel"><div className="panel-heading"><div><span className="eyebrow">System architecture</span><h2>From data to serving</h2><p>Contract-owned flow; live health appears when an API session is available.</p></div><StatusBadge tone="neutral">Awaiting signal</StatusBadge></div><TopologyGraph label="Mindclade system architecture" nodes={nodes} edges={edges} /></section><aside className="panel attention-panel"><div className="panel-heading"><div><span className="eyebrow">Attention queue</span><h2>Decisions, not noise</h2></div></div><div className="empty-attention"><span aria-hidden="true">✓</span><strong>No session data yet</strong><p>Pending approvals, failed gates, and stalled runs will collect here after connection.</p></div><Link className="panel-link" href="/runs">Open run inventory <span>→</span></Link></aside></div></div>;
}
