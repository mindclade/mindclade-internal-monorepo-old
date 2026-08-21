// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import "@mindclade/libs-ts-design-system/theme.css";
import type { Metadata } from "next";
import type { ReactNode } from "react";
import { AppShell } from "../components/AppShell";
import { ErrorBoundary } from "../components/ErrorBoundary";
import "./globals.css";

export const metadata: Metadata = {
  title: { default: "Command · Mindclade", template: "%s · Mindclade Command" },
  description: "Operational command surface for AI research and model systems.",
};

export default function Layout({ children }: Readonly<{ children: ReactNode }>): ReactNode {
  return <html lang="en"><body><ErrorBoundary><AppShell>{children}</AppShell></ErrorBoundary></body></html>;
}
