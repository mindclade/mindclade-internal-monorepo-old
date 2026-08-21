import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { TopologyGraph } from "@mindclade/libs-ts-charts";
export function ModelTopology({ nodes, edges }) {
    return _jsxs("section", { className: "panel", children: [_jsx("div", { className: "panel-heading", children: _jsxs("div", { children: [_jsx("h2", { children: "Execution topology" }), _jsx("p", { children: "Logical data and compute flow." })] }) }), _jsx(TopologyGraph, { label: "Model execution topology", nodes: nodes, edges: edges })] });
}
//# sourceMappingURL=ModelTopology.js.map