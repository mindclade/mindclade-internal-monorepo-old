import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import Link from "next/link";
import { EvaluationDetail } from "../../../components/EvaluationDetail";
export default async function Page({ params }) {
    const { evaluationId } = await params;
    return _jsxs("div", { className: "page-stack", children: [_jsx("header", { className: "page-heading", children: _jsxs("div", { children: [_jsx(Link, { className: "back-link", href: "/evaluations", children: "\u2190 Evaluations" }), _jsx("span", { className: "eyebrow", children: "Evaluation evidence" }), _jsx("h1", { className: "identifier", children: evaluationId }), _jsx("p", { children: "Independent gate results and immutable evidence identity." })] }) }), _jsx(EvaluationDetail, { evaluationId: evaluationId })] });
}
//# sourceMappingURL=page.js.map