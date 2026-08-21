// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { Metric } from "@mindclade/libs-ts-design-system";

export function ResourceUsage({ gpuUtilization, memoryBytes, costPerHour }: { gpuUtilization: number; memoryBytes: number; costPerHour?: number }): React.ReactNode {
  return <div className="metric-grid"><Metric label="Accelerator" value={`${Math.round(gpuUtilization)}%`} detail="active compute" /><Metric label="HBM" value={`${(memoryBytes / 2 ** 30).toFixed(1)} GiB`} detail="allocated" /><Metric label="Run rate" value={costPerHour === undefined ? "—" : `$${costPerHour.toFixed(2)}`} detail="per hour" /></div>;
}
