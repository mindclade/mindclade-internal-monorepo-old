// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
"use client";
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Button } from "@mindclade/libs-ts-design-system";
import { Component } from "react";
export class ErrorBoundary extends Component {
    state = { error: undefined };
    static getDerivedStateFromError(error) { return { error }; }
    componentDidCatch(error, info) { console.error("Console render failure", error, info.componentStack); }
    render() {
        if (this.state.error === undefined)
            return this.props.children;
        return _jsxs("section", { className: "state-message state-message--error", children: [_jsx("span", { children: "Interface interrupted" }), _jsx("h1", { children: "This view couldn\u2019t be rendered." }), _jsx("p", { children: this.state.error.message }), _jsx(Button, { onClick: () => this.setState({ error: undefined }), children: "Try again" })] });
    }
}
//# sourceMappingURL=ErrorBoundary.js.map