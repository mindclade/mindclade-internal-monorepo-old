// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { StatusBadge } from "@mindclade/libs-ts-design-system";

export interface KernelCheck { name: string; platform: string; status: "qualified" | "testing" | "rejected"; variance?: number }

export function KernelQualification({ checks }: { checks: readonly KernelCheck[] }): React.ReactNode {
  return <ul className="signal-list">{checks.map((check) => <li key={`${check.name}-${check.platform}`}><div><strong>{check.name}</strong><small>{check.platform}{check.variance === undefined ? "" : ` · variance ${check.variance}%`}</small></div><StatusBadge tone={check.status === "qualified" ? "success" : check.status === "rejected" ? "danger" : "running"}>{check.status}</StatusBadge></li>)}</ul>;
}
