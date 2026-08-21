import { jsx as _jsx } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { StatusBadge as Badge } from "@mindclade/libs-ts-design-system";
export function StatusBadge({ status }) {
    const value = status.toLowerCase();
    const tone = ["failed", "revoked"].includes(value) ? "danger" : ["ready", "passed", "succeeded"].includes(value) ? "success" : value === "running" ? "running" : "neutral";
    return _jsx(Badge, { tone: tone, pulse: value === "running", children: status });
}
//# sourceMappingURL=StatusBadge.js.map