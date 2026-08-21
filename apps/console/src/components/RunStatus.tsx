// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { Metric, StatusBadge } from "@mindclade/libs-ts-design-system";
import type { Run } from "@mindclade/sdk-typescript";

export function RunStatus({ run }: { run: Run }): React.ReactNode {
  return <section className="panel"><div className="panel-heading"><h2>{run.name}</h2><StatusBadge tone={run.state === "FAILED" ? "danger" : run.state === "RUNNING" ? "running" : run.state === "SUCCEEDED" ? "success" : "neutral"}>{run.state}</StatusBadge></div><div className="metric-grid"><Metric label="Progress" value={`${Math.round(run.progress * 100)}%`} detail={run.currentStage ?? "Awaiting stage"} /><Metric label="Kind" value={run.kind} /><Metric label="Version" value={run.resourceVersion} /></div><div className="progress-track" aria-label={`${Math.round(run.progress * 100)}% complete`}><span style={{ width: `${Math.max(0, Math.min(run.progress * 100, 100))}%` }} /></div></section>;
}
