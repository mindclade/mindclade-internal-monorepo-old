// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import "@mindclade/libs-ts-design-system/theme.css";
import type { Metadata } from "next";
import { headers } from "next/headers";
import type { ReactNode } from "react";
import { AppShell } from "../components/AppShell";
import { ErrorBoundary } from "../components/ErrorBoundary";
import "./globals.css";

export const metadata: Metadata = { title: { default: "Governance · Mindclade", template: "%s · Mindclade Governance" }, description: "Restricted governance and administrative control surface." };
export default async function Layout({ children }: Readonly<{ children: ReactNode }>): Promise<ReactNode> {
  // Request-bound rendering is required so Next can apply the proxy's nonce to
  // framework and inline hydration assets.
  await headers();
  return <html lang="en"><body><ErrorBoundary><AppShell>{children}</AppShell></ErrorBoundary></body></html>;
}
