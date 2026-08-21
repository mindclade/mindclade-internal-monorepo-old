import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { Metric, StatusBadge } from "@mindclade/libs-ts-design-system";
export function RunStatus({ run }) {
    return _jsxs("section", { className: "panel", children: [_jsxs("div", { className: "panel-heading", children: [_jsx("h2", { children: run.name }), _jsx(StatusBadge, { tone: run.state === "FAILED" ? "danger" : run.state === "RUNNING" ? "running" : run.state === "SUCCEEDED" ? "success" : "neutral", children: run.state })] }), _jsxs("div", { className: "metric-grid", children: [_jsx(Metric, { label: "Progress", value: `${Math.round(run.progress * 100)}%`, detail: run.currentStage ?? "Awaiting stage" }), _jsx(Metric, { label: "Kind", value: run.kind }), _jsx(Metric, { label: "Version", value: run.resourceVersion })] }), _jsx("div", { className: "progress-track", "aria-label": `${Math.round(run.progress * 100)}% complete`, children: _jsx("span", { style: { width: `${Math.max(0, Math.min(run.progress * 100, 100))}%` } }) })] });
}
//# sourceMappingURL=RunStatus.js.map