// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface TimelineStage { name: string; status: "waiting" | "running" | "complete" | "failed"; detail?: string }

export function PreprocessingTimeline({ stages }: { stages: readonly TimelineStage[] }): React.ReactNode {
  return <ol className="timeline" aria-label="Preprocessing stages">{stages.map((stage, index) => <li key={`${stage.name}-${index}`} data-status={stage.status}><span aria-hidden="true">{index + 1}</span><div><strong>{stage.name}</strong>{stage.detail === undefined ? null : <small>{stage.detail}</small>}</div><em>{stage.status}</em></li>)}</ol>;
}
