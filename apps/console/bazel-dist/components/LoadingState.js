import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
export function LoadingState({ label = "Loading workspace data" }) {
    return _jsxs("div", { className: "loading-state", role: "status", children: [_jsxs("span", { className: "loading-orbit", "aria-hidden": "true", children: [_jsx("i", {}), _jsx("i", {}), _jsx("i", {})] }), _jsx("strong", { children: label }), _jsx("small", { children: "Resolving canonical state\u2026" })] });
}
//# sourceMappingURL=LoadingState.js.map