import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { LineChart } from "@mindclade/libs-ts-charts";
export function MetricChart({ label, points, value }) {
    return _jsxs("section", { className: "chart-card", children: [_jsxs("header", { children: [_jsx("span", { children: label }), value === undefined ? null : _jsx("strong", { children: value })] }), _jsx(LineChart, { label: label, points: points })] });
}
//# sourceMappingURL=MetricChart.js.map