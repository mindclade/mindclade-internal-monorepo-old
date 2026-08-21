// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { LineChart, type LinePoint } from "@mindclade/libs-ts-charts";

export function MetricChart({ label, points, value }: { label: string; points: readonly LinePoint[]; value?: string }): React.ReactNode {
  return <section className="chart-card"><header><span>{label}</span>{value === undefined ? null : <strong>{value}</strong>}</header><LineChart label={label} points={points} /></section>;
}
