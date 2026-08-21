import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { StatusBadge } from "@mindclade/libs-ts-design-system";
export function RolloutFleet({ targets }) {
    return _jsx("ul", { className: "signal-list", children: targets.map((target) => _jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: target.region }), _jsxs("small", { children: [target.version, " \u00B7 ", target.healthy, "/", target.replicas, " replicas"] })] }), _jsx(StatusBadge, { tone: target.healthy === target.replicas ? "success" : "warning", children: target.healthy === target.replicas ? "Healthy" : "Converging" })] }, target.region)) });
}
//# sourceMappingURL=RolloutFleet.js.map