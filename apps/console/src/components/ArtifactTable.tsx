// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { DataTable, StatusBadge, type Column } from "@mindclade/libs-ts-design-system";
import type { Artifact } from "@mindclade/sdk-typescript";
import { formatBytes, formatRelativeTime } from "../lib/format";

export function ArtifactTable({ artifacts }: { artifacts: readonly Artifact[] }): React.ReactNode {
  const columns: readonly Column<Artifact>[] = [
    { key: "digest", header: "Digest", cell: (item) => <code>{item.digest.slice(0, 22)}</code> },
    { key: "kind", header: "Kind", cell: (item) => item.kind },
    { key: "size", header: "Size", align: "end", cell: (item) => formatBytes(item.sizeBytes) },
    { key: "status", header: "Trust", cell: (item) => <StatusBadge tone={item.verificationStatus === "VERIFIED" ? "success" : item.verificationStatus === "QUARANTINED" ? "danger" : "neutral"}>{item.verificationStatus}</StatusBadge> },
    { key: "time", header: "Created", align: "end", cell: (item) => formatRelativeTime(item.createdAt) },
  ];
  return <DataTable caption="Content-addressed artifacts" columns={columns} rows={artifacts} rowKey={(item) => item.digest} />;
}
