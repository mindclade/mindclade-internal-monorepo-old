// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
"use client";
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Button } from "@mindclade/libs-ts-design-system";
import { useEffect, useState } from "react";
import { apiClient } from "../lib/api";
import { EvaluationSummary } from "./EvaluationSummary";
import { LoadingState } from "./LoadingState";
export function EvaluationDetail({ evaluationId }) {
    const [state, setState] = useState({ status: "loading" });
    useEffect(() => {
        const controller = new AbortController();
        apiClient().evaluations.get(evaluationId, controller.signal).then((evaluation) => setState({ status: "ready", evaluation })).catch((cause) => {
            if (!controller.signal.aborted)
                setState({ status: "error", message: cause instanceof Error ? cause.message : "Evaluation request failed" });
        });
        return () => controller.abort();
    }, [evaluationId]);
    if (state.status === "loading")
        return _jsx(LoadingState, { label: "Loading evaluation evidence" });
    if (state.status === "error")
        return _jsxs("div", { className: "state-message state-message--error", children: [_jsx("span", { children: "Evaluation unavailable" }), _jsx("h2", { children: "Evidence could not be loaded." }), _jsx("p", { children: state.message }), _jsx(Button, { onClick: () => location.reload(), children: "Retry" })] });
    return _jsx(EvaluationSummary, { evaluation: state.evaluation });
}
//# sourceMappingURL=EvaluationDetail.js.map