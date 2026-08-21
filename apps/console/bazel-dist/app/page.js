import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { TopologyGraph } from "@mindclade/libs-ts-charts";
import { Button, Metric, StatusBadge } from "@mindclade/libs-ts-design-system";
import Link from "next/link";
const nodes = [
    { id: "data", label: "Data", x: 82, y: 132 },
    { id: "train", label: "Training", x: 238, y: 68, tone: "active" },
    { id: "eval", label: "Evaluation", x: 398, y: 132 },
    { id: "serve", label: "Serving", x: 556, y: 68 },
    { id: "artifacts", label: "Artifacts", x: 318, y: 224 },
];
const edges = [{ from: "data", to: "train" }, { from: "train", to: "eval" }, { from: "eval", to: "serve" }, { from: "train", to: "artifacts" }, { from: "eval", to: "artifacts" }];
export default function Page() {
    return _jsxs("div", { className: "page-stack dashboard", children: [_jsxs("header", { className: "command-hero", children: [_jsxs("div", { children: [_jsx("span", { className: "eyebrow", children: "AI systems command" }), _jsxs("h1", { children: ["See the whole machine.", _jsx("br", {}), _jsx("em", { children: "Move with evidence." })] }), _jsx("p", { children: "One operational surface for durable runs, qualified models, immutable data, and the proof behind every promotion." }), _jsxs("div", { className: "hero-actions", children: [_jsx(Button, { tone: "primary", disabled: true, title: "Run creation requires a connected API session", children: "New run" }), _jsx(Link, { className: "text-link", href: "/evaluations", children: "Review evaluations \u2192" })] })] }), _jsxs("div", { className: "system-radar", "aria-hidden": "true", children: [_jsx("span", {}), _jsx("span", {}), _jsx("span", {}), _jsx("i", {})] })] }), _jsxs("section", { className: "metric-strip", "aria-label": "Workspace signal", children: [_jsx(Metric, { label: "Active runs", value: "\u2014", detail: "Connect an environment" }), _jsx(Metric, { label: "Qualified models", value: "\u2014", detail: "Canonical registry" }), _jsx(Metric, { label: "Evaluation gates", value: "\u2014", detail: "Independent evidence" }), _jsx(Metric, { label: "Artifact trust", value: "\u2014", detail: "Content addressed" })] }), _jsxs("div", { className: "dashboard-grid", children: [_jsxs("section", { className: "panel topology-panel", children: [_jsxs("div", { className: "panel-heading", children: [_jsxs("div", { children: [_jsx("span", { className: "eyebrow", children: "System architecture" }), _jsx("h2", { children: "From data to serving" }), _jsx("p", { children: "Contract-owned flow; live health appears when an API session is available." })] }), _jsx(StatusBadge, { tone: "neutral", children: "Awaiting signal" })] }), _jsx(TopologyGraph, { label: "Mindclade system architecture", nodes: nodes, edges: edges })] }), _jsxs("aside", { className: "panel attention-panel", children: [_jsx("div", { className: "panel-heading", children: _jsxs("div", { children: [_jsx("span", { className: "eyebrow", children: "Attention queue" }), _jsx("h2", { children: "Decisions, not noise" })] }) }), _jsxs("div", { className: "empty-attention", children: [_jsx("span", { "aria-hidden": "true", children: "\u2713" }), _jsx("strong", { children: "No session data yet" }), _jsx("p", { children: "Pending approvals, failed gates, and stalled runs will collect here after connection." })] }), _jsxs(Link, { className: "panel-link", href: "/runs", children: ["Open run inventory ", _jsx("span", { children: "\u2192" })] })] })] })] });
}
//# sourceMappingURL=page.js.map