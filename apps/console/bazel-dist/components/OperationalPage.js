import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
import { Button, StatusBadge } from "@mindclade/libs-ts-design-system";
export function OperationalPage({ eyebrow, title, description, capability, action }) {
    return _jsxs("div", { className: "page-stack", children: [_jsxs("header", { className: "page-heading", children: [_jsxs("div", { children: [_jsx("span", { className: "eyebrow", children: eyebrow }), _jsx("h1", { children: title }), _jsx("p", { children: description })] }), action === undefined ? null : _jsx(Button, { tone: "primary", disabled: true, title: "Available after the owning API is connected", children: action })] }), _jsxs("section", { className: "panel capability-state", children: [_jsx("div", { className: "capability-icon", "aria-hidden": "true", children: "\u2301" }), _jsx(StatusBadge, { tone: "info", children: "Contract boundary" }), _jsx("h2", { children: capability }), _jsx("p", { children: "This surface is ready for its owning service contract. It won\u2019t invent operational state or bypass the platform\u2019s trust boundary." }), _jsx(Button, { tone: "quiet", disabled: true, title: "Integration documentation has not been published", children: "Integration guide pending" })] })] });
}
//# sourceMappingURL=OperationalPage.js.map