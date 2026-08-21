// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
"use client";
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { anonymousIdentity } from "../lib/auth";
const groups = [
    { label: "Operate", items: [["/runs", "Runs"], ["/clusters", "Compute"], ["/serving", "Serving"], ["/rollouts", "Rollouts"]] },
    { label: "Build", items: [["/models", "Models"], ["/datasets", "Datasets"], ["/experiments", "Experiments"], ["/preprocessing", "Preprocessing"]] },
    { label: "Assure", items: [["/evaluations", "Evaluations"], ["/safety", "Safety"], ["/artifacts", "Artifacts"], ["/checkpoints", "Checkpoints"], ["/kernels", "Kernels"]] },
];
export function AppShell({ children }) {
    const pathname = usePathname();
    return (_jsxs("div", { className: "shell", children: [_jsx("a", { className: "skip-link", href: "#content", children: "Skip to content" }), _jsxs("aside", { className: "sidebar", children: [_jsxs(Link, { className: "brand", href: "/", "aria-label": "Mindclade command home", children: [_jsx("span", { className: "brand-mark", "aria-hidden": "true", children: "M" }), _jsxs("span", { children: ["Mindclade", _jsx("small", { children: "Command" })] })] }), _jsx("nav", { "aria-label": "Primary navigation", children: groups.map((group) => _jsxs("section", { className: "nav-group", children: [_jsx("h2", { children: group.label }), group.items.map(([href, label]) => _jsxs(Link, { href: href, "aria-current": pathname === href || pathname.startsWith(`${href}/`) ? "page" : undefined, children: [_jsx("span", { "aria-hidden": "true" }), label] }, href))] }, group.label)) }), _jsxs("div", { className: "identity", children: [_jsx("span", { className: "identity-orb", "aria-hidden": "true" }), _jsxs("div", { children: [_jsx("strong", { children: anonymousIdentity.displayName }), _jsxs("small", { children: [anonymousIdentity.environment, " \u00B7 ", anonymousIdentity.organization] })] })] })] }), _jsxs("div", { className: "workspace", children: [_jsxs("header", { className: "topbar", children: [_jsxs("div", { className: "environment", children: [_jsx("span", { "aria-hidden": "true" }), "Environment ", _jsx("strong", { children: anonymousIdentity.environment })] }), _jsx("button", { className: "icon-button", type: "button", disabled: true, title: "Notifications require a connected session", "aria-label": "Notifications unavailable", children: "\u2014" })] }), _jsx("main", { id: "content", className: "content", tabIndex: -1, children: children })] })] }));
}
//# sourceMappingURL=AppShell.js.map