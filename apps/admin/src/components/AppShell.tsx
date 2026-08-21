// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { adminContext } from "../lib/auth";

const links = [["/", "Control room"], ["/users", "Users"], ["/organizations", "Organizations"], ["/service-accounts", "Service accounts"], ["/quotas", "Quotas"], ["/evaluations", "Evaluation gates"], ["/releases", "Releases"], ["/model-weights", "Model weights"], ["/audit", "Audit trail"], ["/break-glass", "Break glass"]] as const;

export function AppShell({ children }: { children: ReactNode }): ReactNode {
  const pathname = usePathname();
  return <div className="admin-shell"><a className="skip-link" href="#admin-content">Skip to content</a><aside className="admin-sidebar"><Link className="admin-brand" href="/"><span>MC</span><div>Mindclade<small>Governance control</small></div></Link><div className="admin-boundary"><span>Restricted surface</span><strong>{adminContext.environment}</strong></div><nav aria-label="Administration">{links.map(([href, label]) => <Link key={href} href={href} aria-current={pathname === href ? "page" : undefined}>{label}<span aria-hidden="true">›</span></Link>)}</nav><div className="admin-identity"><span aria-hidden="true" /><div><strong>{adminContext.operator}</strong><small>{adminContext.assurance} assurance</small></div></div></aside><div className="admin-workspace"><header className="admin-topbar"><span><i aria-hidden="true" /> All administrative mutations are audited</span><strong>Read-only contract</strong></header><main id="admin-content" className="admin-content" tabIndex={-1}>{children}</main></div></div>;
}
