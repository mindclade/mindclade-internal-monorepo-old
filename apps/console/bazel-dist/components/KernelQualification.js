import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { StatusBadge } from "@mindclade/libs-ts-design-system";
export function KernelQualification({ checks }) {
    return _jsx("ul", { className: "signal-list", children: checks.map((check) => _jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: check.name }), _jsxs("small", { children: [check.platform, check.variance === undefined ? "" : ` · variance ${check.variance}%`] })] }), _jsx(StatusBadge, { tone: check.status === "qualified" ? "success" : check.status === "rejected" ? "danger" : "running", children: check.status })] }, `${check.name}-${check.platform}`)) });
}
//# sourceMappingURL=KernelQualification.js.map