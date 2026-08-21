// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { DataTable, StatusBadge, type Column } from "@mindclade/libs-ts-design-system";
import { formatBytes, formatRelativeTime } from "../lib/format";

export interface CheckpointRow { digest: string; step: number; sizeBytes: number; createdAt: string; verified: boolean }

export function CheckpointTable({ checkpoints }: { checkpoints: readonly CheckpointRow[] }): React.ReactNode {
  const columns: readonly Column<CheckpointRow>[] = [
    { key: "digest", header: "Checkpoint", cell: (item) => <code>{item.digest.slice(0, 20)}</code> },
    { key: "step", header: "Step", align: "end", cell: (item) => item.step.toLocaleString() },
    { key: "size", header: "Size", align: "end", cell: (item) => formatBytes(item.sizeBytes) },
    { key: "trust", header: "Trust", cell: (item) => <StatusBadge tone={item.verified ? "success" : "warning"}>{item.verified ? "Verified" : "Pending"}</StatusBadge> },
    { key: "time", header: "Created", align: "end", cell: (item) => formatRelativeTime(item.createdAt) },
  ];
  return <DataTable caption="Training checkpoints" columns={columns} rows={checkpoints} rowKey={(item) => item.digest} />;
}
