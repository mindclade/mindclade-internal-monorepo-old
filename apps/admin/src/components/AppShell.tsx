// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useAdminContext } from "../lib/auth";

const links = [["/", "Control room"], ["/users", "Users"], ["/organizations", "Organizations"], ["/service-accounts", "Service accounts"], ["/quotas", "Quotas"], ["/evaluations", "Evaluation gates"], ["/releases", "Releases"], ["/model-weights", "Model weights"], ["/audit", "Audit trail"], ["/break-glass", "Break glass"]] as const;

export function AppShell({ children }: { children: ReactNode }): ReactNode {
  const pathname = usePathname();
  const adminContext = useAdminContext();
  const [navigationOpen, setNavigationOpen] = useState(false);
  const navigationButton = useRef<HTMLButtonElement>(null);
  useEffect(() => setNavigationOpen(false), [pathname]);
  useEffect(() => {
    if (!navigationOpen) return;
    const closeNavigation = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") return;
      setNavigationOpen(false);
      navigationButton.current?.focus();
    };
    document.addEventListener("keydown", closeNavigation);
    return () => document.removeEventListener("keydown", closeNavigation);
  }, [navigationOpen]);
  return <div className="admin-shell"><a className="skip-link" href="#admin-content">Skip to content</a><aside className="admin-sidebar" data-open={navigationOpen || undefined}><Link className="admin-brand" href="/"><span>MC</span><div>Mindclade<small>Governance control</small></div></Link><button ref={navigationButton} className="mobile-nav-toggle" type="button" aria-controls="admin-navigation" aria-expanded={navigationOpen} onClick={() => setNavigationOpen((open) => !open)}><span aria-hidden="true" />{navigationOpen ? "Close" : "Menu"}</button><div className="admin-boundary"><span>Restricted surface</span><strong>{adminContext.environment}</strong></div><nav id="admin-navigation" aria-label="Administration">{links.map(([href, label]) => <Link key={href} href={href} aria-current={pathname === href ? "page" : undefined}>{label}<span aria-hidden="true">›</span></Link>)}</nav><div className="admin-identity" data-session={adminContext.status}><span aria-hidden="true" /><div><strong>{adminContext.operator}</strong><small>{adminContext.assurance} assurance</small></div></div></aside><div className="admin-workspace"><header className="admin-topbar"><span><i aria-hidden="true" /> Mutations require an audit contract</span><strong>Mutations disabled</strong></header><main id="admin-content" className="admin-content" tabIndex={-1}>{children}</main></div></div>;
}
