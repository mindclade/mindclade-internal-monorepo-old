import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { Metric } from "@mindclade/libs-ts-design-system";
export function ResourceUsage({ gpuUtilization, memoryBytes, costPerHour }) {
    return _jsxs("div", { className: "metric-grid", children: [_jsx(Metric, { label: "Accelerator", value: `${Math.round(gpuUtilization)}%`, detail: "active compute" }), _jsx(Metric, { label: "HBM", value: `${(memoryBytes / 2 ** 30).toFixed(1)} GiB`, detail: "allocated" }), _jsx(Metric, { label: "Run rate", value: costPerHour === undefined ? "—" : `$${costPerHour.toFixed(2)}`, detail: "per hour" })] });
}
//# sourceMappingURL=ResourceUsage.js.map