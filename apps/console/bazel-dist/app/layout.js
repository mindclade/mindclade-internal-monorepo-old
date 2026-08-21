import { jsx as _jsx } from "react/jsx-runtime";
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
import "@mindclade/libs-ts-design-system/theme.css";
import { AppShell } from "../components/AppShell";
import { ErrorBoundary } from "../components/ErrorBoundary";
import "./globals.css";
export const metadata = {
    title: { default: "Command · Mindclade", template: "%s · Mindclade Command" },
    description: "Operational command surface for AI research and model systems.",
};
export default function Layout({ children }) {
    return _jsx("html", { lang: "en", children: _jsx("body", { children: _jsx(ErrorBoundary, { children: _jsx(AppShell, { children: children }) }) }) });
}
//# sourceMappingURL=layout.js.map