// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { PreprocessingTimeline, type TimelineStage } from "./PreprocessingTimeline";

export function TrainingTimeline({ stages }: { stages: readonly TimelineStage[] }): React.ReactNode {
  return <section className="panel"><div className="panel-heading"><h2>Training timeline</h2></div><PreprocessingTimeline stages={stages} /></section>;
}
