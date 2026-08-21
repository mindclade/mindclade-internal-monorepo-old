import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { StatusBadge } from "@mindclade/libs-ts-design-system";
export function SafetyGate({ checks }) {
    const blocked = checks.some((check) => check.result === "fail");
    return _jsxs("section", { className: "panel", children: [_jsxs("div", { className: "panel-heading", children: [_jsxs("div", { children: [_jsx("h2", { children: "Safety gate" }), _jsx("p", { children: "Independent evidence required before promotion." })] }), _jsx(StatusBadge, { tone: blocked ? "danger" : checks.every((check) => check.result === "pass") ? "success" : "warning", children: blocked ? "Blocked" : "Reviewing" })] }), _jsx("ul", { className: "signal-list", children: checks.map((check) => _jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: check.name }), _jsx("small", { children: check.evidence ?? "Evidence not attached" })] }), _jsx(StatusBadge, { tone: check.result === "pass" ? "success" : check.result === "fail" ? "danger" : "warning", children: check.result })] }, check.name)) })] });
}
//# sourceMappingURL=SafetyGate.js.map