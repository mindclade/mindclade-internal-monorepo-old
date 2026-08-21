import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import { MolecularViewer } from "@mindclade/libs-ts-molecular-viewer";
export function MolecularResult({ structure }) {
    return _jsxs("section", { className: "panel", children: [_jsx("div", { className: "panel-heading", children: _jsxs("div", { children: [_jsx("h2", { children: "Predicted structure" }), _jsxs("p", { children: [structure.atoms.length.toLocaleString(), " atoms \u00B7 deterministic projection"] })] }) }), _jsx(MolecularViewer, { structure: structure })] });
}
//# sourceMappingURL=MolecularResult.js.map