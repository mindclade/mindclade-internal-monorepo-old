import { jsx as _jsx } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { DataTable, StatusBadge } from "@mindclade/libs-ts-design-system";
import { formatBytes, formatRelativeTime } from "../lib/format";
export function ArtifactTable({ artifacts }) {
    const columns = [
        { key: "digest", header: "Digest", cell: (item) => _jsx("code", { children: item.digest.slice(0, 22) }) },
        { key: "kind", header: "Kind", cell: (item) => item.kind },
        { key: "size", header: "Size", align: "end", cell: (item) => formatBytes(item.sizeBytes) },
        { key: "status", header: "Trust", cell: (item) => _jsx(StatusBadge, { tone: item.verificationStatus === "VERIFIED" ? "success" : item.verificationStatus === "QUARANTINED" ? "danger" : "neutral", children: item.verificationStatus }) },
        { key: "time", header: "Created", align: "end", cell: (item) => formatRelativeTime(item.createdAt) },
    ];
    return _jsx(DataTable, { caption: "Content-addressed artifacts", columns: columns, rows: artifacts, rowKey: (item) => item.digest });
}
//# sourceMappingURL=ArtifactTable.js.map