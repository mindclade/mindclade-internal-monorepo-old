// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { Metric, StatusBadge } from "@mindclade/libs-ts-design-system";
import Link from "next/link";

const controls = [["/evaluations", "Evaluation approvals", "Independent evidence before promotion"], ["/releases", "Release promotions", "Immutable release pointer changes"], ["/model-weights", "Weight access", "Bounded access to sensitive artifacts"], ["/break-glass", "Emergency access", "Time-limited, reviewed elevation"]] as const;

export default function Page(): React.ReactNode {
  return <div className="admin-page"><header className="admin-hero"><div><span className="admin-eyebrow">Governance control room</span><h1>Authority should<br />leave a trail.</h1><p>High-stakes actions are explicit, evidence-bound, idempotent, and reviewable. This surface fails closed when policy state is unavailable.</p></div><div className="assurance-card"><span>Session assurance</span><StatusBadge tone="neutral">Unverified</StatusBadge><p>The shell verifies the cookie-backed session at runtime; mutation controls remain disabled without connected policy.</p></div></header><section className="admin-metrics"><Metric label="Pending approvals" value="—" detail="Connect admin API" /><Metric label="Policy exceptions" value="—" detail="No fabricated state" /><Metric label="Audit ingestion" value="—" detail="Awaiting contract" /></section><section className="admin-panel control-index"><header><div><span className="admin-eyebrow">Critical controls</span><h2>Mutation boundaries</h2></div></header>{controls.map(([href, title, detail]) => <Link key={href} href={href}><span aria-hidden="true">0{controls.findIndex((item) => item[0] === href) + 1}</span><div><strong>{title}</strong><small>{detail}</small></div><i aria-hidden="true">→</i></Link>)}</section></div>;
}
