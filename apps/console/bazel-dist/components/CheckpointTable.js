import { jsx as _jsx } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { DataTable, StatusBadge } from "@mindclade/libs-ts-design-system";
import { formatBytes, formatRelativeTime } from "../lib/format";
export function CheckpointTable({ checkpoints }) {
    const columns = [
        { key: "digest", header: "Checkpoint", cell: (item) => _jsx("code", { children: item.digest.slice(0, 20) }) },
        { key: "step", header: "Step", align: "end", cell: (item) => item.step.toLocaleString() },
        { key: "size", header: "Size", align: "end", cell: (item) => formatBytes(item.sizeBytes) },
        { key: "trust", header: "Trust", cell: (item) => _jsx(StatusBadge, { tone: item.verified ? "success" : "warning", children: item.verified ? "Verified" : "Pending" }) },
        { key: "time", header: "Created", align: "end", cell: (item) => formatRelativeTime(item.createdAt) },
    ];
    return _jsx(DataTable, { caption: "Training checkpoints", columns: columns, rows: checkpoints, rowKey: (item) => item.digest });
}
//# sourceMappingURL=CheckpointTable.js.map