// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
"use client";
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Button } from "@mindclade/libs-ts-design-system";
import { useEffect, useState } from "react";
import { apiClient } from "../lib/api";
import { LoadingState } from "./LoadingState";
import { RunStatus } from "./RunStatus";
export function RunDetail({ runId }) {
    const [state, setState] = useState({ status: "loading" });
    useEffect(() => {
        const controller = new AbortController();
        apiClient().runs.get(runId, { signal: controller.signal }).then((run) => setState({ status: "ready", run })).catch((cause) => {
            if (!controller.signal.aborted)
                setState({ status: "error", message: cause instanceof Error ? cause.message : "Run request failed" });
        });
        return () => controller.abort();
    }, [runId]);
    if (state.status === "loading")
        return _jsx(LoadingState, { label: "Loading run state" });
    if (state.status === "error")
        return _jsxs("div", { className: "state-message state-message--error", children: [_jsx("span", { children: "Run unavailable" }), _jsx("h2", { children: "Canonical state could not be loaded." }), _jsx("p", { children: state.message }), _jsx(Button, { onClick: () => location.reload(), children: "Retry" })] });
    return _jsx(RunStatus, { run: state.run });
}
//# sourceMappingURL=RunDetail.js.map