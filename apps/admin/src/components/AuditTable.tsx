// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { DataTable, StatusBadge, type Column } from "@mindclade/libs-ts-design-system";
import { formatAuditTime, shortIdentity } from "../lib/format";
import type { AuditRecord } from "../lib/types";

export function AuditTable({ records }: { records: readonly AuditRecord[] }): React.ReactNode {
  const columns: readonly Column<AuditRecord>[] = [
    { key: "time", header: "Time", cell: (row) => formatAuditTime(row.occurredAt) },
    { key: "actor", header: "Actor", cell: (row) => <code>{shortIdentity(row.actor)}</code> },
    { key: "action", header: "Action", cell: (row) => row.action },
    { key: "resource", header: "Resource", cell: (row) => <code>{shortIdentity(row.resource)}</code> },
    { key: "outcome", header: "Outcome", cell: (row) => <StatusBadge tone={row.outcome === "allowed" ? "success" : row.outcome === "denied" ? "warning" : "danger"}>{row.outcome}</StatusBadge> },
  ];
  return <DataTable caption="Administrative audit trail" columns={columns} rows={records} rowKey={(row) => row.id} />;
}
