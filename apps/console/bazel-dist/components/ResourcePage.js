// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
"use client";
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Button, DataTable, StatusBadge } from "@mindclade/libs-ts-design-system";
import Link from "next/link";
import { useEffect, useState } from "react";
import { loadResources } from "../lib/api";
import { formatRelativeTime } from "../lib/format";
import { LoadingState } from "./LoadingState";
function tone(status) {
    const value = status.toLowerCase();
    if (["failed", "revoked", "quarantined"].includes(value))
        return "danger";
    if (["blocked", "cancelling", "candidate"].includes(value))
        return "warning";
    if (["running", "building", "preparing"].includes(value))
        return "running";
    if (["passed", "ready", "succeeded", "deployed", "verified", "qualified"].includes(value))
        return "success";
    return "neutral";
}
export function ResourcePage({ kind, copy }) {
    const [state, setState] = useState({ status: "loading" });
    useEffect(() => {
        const controller = new AbortController();
        loadResources(kind, controller.signal).then((rows) => setState({ status: "ready", rows })).catch((cause) => {
            if (!controller.signal.aborted)
                setState({ status: "error", message: cause instanceof Error ? cause.message : "Resource request failed" });
        });
        return () => controller.abort();
    }, [kind]);
    const columns = [
        { key: "name", header: "Name", cell: (row) => row.href === undefined ? _jsx("strong", { className: "table-primary", children: row.name }) : _jsx(Link, { className: "table-link", href: row.href, children: row.name }) },
        { key: "kind", header: "Kind", cell: (row) => _jsx("span", { className: "mono", children: row.kind }) },
        { key: "status", header: "State", cell: (row) => _jsx(StatusBadge, { tone: tone(row.status), pulse: row.status.toLowerCase() === "running", children: row.status }) },
        { key: "time", header: "Updated", align: "end", cell: (row) => formatRelativeTime(row.updatedAt) },
    ];
    return (_jsxs("div", { className: "page-stack", children: [_jsxs("header", { className: "page-heading", children: [_jsxs("div", { children: [_jsx("span", { className: "eyebrow", children: copy.eyebrow }), _jsx("h1", { children: copy.title }), _jsx("p", { children: copy.description })] }), copy.action === undefined ? null : _jsx(Button, { tone: "primary", disabled: true, title: "Available after the creation contract is connected", children: copy.action })] }), _jsxs("section", { className: "panel resource-panel", "aria-live": "polite", children: [_jsxs("div", { className: "panel-heading", children: [_jsxs("div", { children: [_jsx("h2", { children: "Workspace inventory" }), _jsx("p", { children: "Canonical resources visible to this session." })] }), _jsxs("span", { className: "live-label", children: [_jsx("i", { "aria-hidden": "true" }), " Live API"] })] }), state.status === "loading" ? _jsx(LoadingState, { label: `Loading ${kind}` }) : state.status === "error" ? _jsxs("div", { className: "state-message state-message--error", children: [_jsx("span", { children: "Connection interrupted" }), _jsxs("h3", { children: ["We couldn\u2019t load ", kind, "."] }), _jsx("p", { children: state.message }), _jsx(Button, { onClick: () => location.reload(), children: "Retry" })] }) : _jsx(DataTable, { caption: `${copy.title} inventory`, columns: columns, rows: state.rows, rowKey: (row) => row.id, empty: _jsxs("div", { className: "empty-inline", children: [_jsx("strong", { children: copy.emptyTitle }), _jsx("span", { children: copy.emptyDetail })] }) })] })] }));
}
//# sourceMappingURL=ResourcePage.js.map