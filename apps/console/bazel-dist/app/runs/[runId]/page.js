import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import Link from "next/link";
import { RunDetail } from "../../../components/RunDetail";
export default async function Page({ params }) {
    const { runId } = await params;
    return _jsxs("div", { className: "page-stack", children: [_jsx("header", { className: "page-heading", children: _jsxs("div", { children: [_jsx(Link, { className: "back-link", href: "/runs", children: "\u2190 Runs" }), _jsx("span", { className: "eyebrow", children: "Run detail" }), _jsx("h1", { className: "identifier", children: runId }), _jsx("p", { children: "Canonical orchestration state and committed outputs." })] }) }), _jsx(RunDetail, { runId: runId })] });
}
//# sourceMappingURL=page.js.map