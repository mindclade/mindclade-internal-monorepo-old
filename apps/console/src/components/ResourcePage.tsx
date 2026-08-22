// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { Button, DataTable, StatusBadge, type Column, type StatusTone } from "@mindclade/libs-ts-design-system";
import Link from "next/link";
import { useEffect, useState } from "react";
import { loadResources } from "../lib/api";
import { formatRelativeTime } from "../lib/format";
import type { PublicResourceKind, ResourcePageCopy, ResourceRow } from "../lib/types";
import { LoadingState } from "./LoadingState";

function tone(status: string): StatusTone {
  const value = status.toLowerCase();
  if (["failed", "revoked", "quarantined"].includes(value)) return "danger";
  if (["blocked", "cancelling", "candidate"].includes(value)) return "warning";
  if (["running", "building", "preparing"].includes(value)) return "running";
  if (["passed", "ready", "succeeded", "deployed", "verified", "qualified"].includes(value)) return "success";
  return "neutral";
}

export function ResourcePage({ kind, copy }: { kind: PublicResourceKind; copy: ResourcePageCopy }): React.ReactNode {
  const [state, setState] = useState<{ status: "loading" } | { status: "ready"; rows: ResourceRow[] } | { status: "error"; message: string }>({ status: "loading" });
  useEffect(() => {
    const controller = new AbortController();
    loadResources(kind, controller.signal).then((rows) => setState({ status: "ready", rows })).catch((cause: unknown) => {
      if (!controller.signal.aborted) setState({ status: "error", message: cause instanceof Error ? cause.message : "Resource request failed" });
    });
    return () => controller.abort();
  }, [kind]);

  const columns: readonly Column<ResourceRow>[] = [
    { key: "name", header: "Name", cell: (row) => row.href === undefined ? <strong className="table-primary">{row.name}</strong> : <Link className="table-link" href={row.href}>{row.name}</Link> },
    { key: "kind", header: "Kind", cell: (row) => <span className="mono">{row.kind}</span> },
    { key: "status", header: "State", cell: (row) => <StatusBadge tone={tone(row.status)} pulse={row.status.toLowerCase() === "running"}>{row.status}</StatusBadge> },
    { key: "time", header: "Updated", align: "end", cell: (row) => formatRelativeTime(row.updatedAt) },
  ];
  return (
    <div className="page-stack">
      <header className="page-heading"><div><span className="eyebrow">{copy.eyebrow}</span><h1>{copy.title}</h1><p>{copy.description}</p></div>{copy.action === undefined ? null : <Button tone="primary" disabled title="Available after the creation contract is connected">{copy.action}</Button>}</header>
      <section className="panel resource-panel" aria-live="polite">
        <div className="panel-heading"><div><h2>Workspace inventory</h2><p>Canonical resources visible to this session.</p></div><span className="live-label"><i aria-hidden="true" /> Live API</span></div>
        {state.status === "loading" ? <LoadingState label={`Loading ${kind}`} /> : state.status === "error" ? <div className="state-message state-message--error"><span>Connection interrupted</span><h3>We couldn’t load {kind}.</h3><p>{state.message}</p><Button onClick={() => location.reload()}>Retry</Button></div> : <DataTable caption={`${copy.title} inventory`} columns={columns} rows={state.rows} rowKey={(row) => row.id} empty={<div className="empty-inline"><strong>{copy.emptyTitle}</strong><span>{copy.emptyDetail}</span></div>} />}
      </section>
    </div>
  );
}
