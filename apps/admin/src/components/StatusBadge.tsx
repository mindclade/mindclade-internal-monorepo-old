// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { StatusBadge as Badge, type StatusTone } from "@mindclade/libs-ts-design-system";

export function StatusBadge({ status }: { status: string }): React.ReactNode {
  const value = status.toLowerCase();
  const tone: StatusTone = ["denied", "failed", "revoked"].includes(value) ? "danger" : ["allowed", "active", "approved"].includes(value) ? "success" : "warning";
  return <Badge tone={tone}>{status}</Badge>;
}
