import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { Metric, StatusBadge } from "@mindclade/libs-ts-design-system";
export function EvaluationSummary({ evaluation }) {
    return _jsxs("section", { className: "panel", children: [_jsxs("div", { className: "panel-heading", children: [_jsxs("div", { children: [_jsx("span", { className: "eyebrow", children: "Evaluation suite" }), _jsx("h2", { children: evaluation.suite })] }), _jsx(StatusBadge, { tone: evaluation.status === "PASSED" ? "success" : evaluation.status === "FAILED" ? "danger" : "warning", children: evaluation.status })] }), _jsx("div", { className: "metric-grid", children: evaluation.metrics.map((metric) => _jsx(Metric, { label: metric.name, value: metric.value.toLocaleString(), detail: `${metric.unit}${metric.threshold === undefined ? "" : ` · threshold ${metric.threshold}`}`, trend: metric.passed ? "up" : "down" }, metric.name)) }), _jsxs("p", { className: "evidence", children: ["Evidence ", _jsx("code", { children: evaluation.evidenceDigest }), evaluation.holdoutProtected ? " · holdout protected" : ""] })] });
}
//# sourceMappingURL=EvaluationSummary.js.map