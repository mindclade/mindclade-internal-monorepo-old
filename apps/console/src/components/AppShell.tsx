// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useConsoleIdentity } from "../lib/auth";

const groups = [
  { label: "Operate", items: [["/runs", "Runs"], ["/clusters", "Compute"], ["/serving", "Serving"], ["/rollouts", "Rollouts"]] },
  { label: "Build", items: [["/models", "Models"], ["/datasets", "Datasets"], ["/experiments", "Experiments"], ["/preprocessing", "Preprocessing"]] },
  { label: "Assure", items: [["/evaluations", "Evaluations"], ["/safety", "Safety"], ["/artifacts", "Artifacts"], ["/checkpoints", "Checkpoints"], ["/kernels", "Kernels"]] },
] as const;

export function AppShell({ children }: { children: ReactNode }): ReactNode {
  const pathname = usePathname();
  const identity = useConsoleIdentity();
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
  return (
    <div className="shell">
      <a className="skip-link" href="#content">Skip to content</a>
      <aside className="sidebar" data-open={navigationOpen || undefined}>
        <Link className="brand" href="/" aria-label="Mindclade command home"><span className="brand-mark" aria-hidden="true">M</span><span>Mindclade<small>Command</small></span></Link>
        <button ref={navigationButton} className="mobile-nav-toggle" type="button" aria-controls="console-navigation" aria-expanded={navigationOpen} onClick={() => setNavigationOpen((open) => !open)}><span aria-hidden="true" />{navigationOpen ? "Close" : "Menu"}</button>
        <nav id="console-navigation" aria-label="Primary navigation">
          {groups.map((group) => <section className="nav-group" key={group.label}><h2>{group.label}</h2>{group.items.map(([href, label]) => <Link key={href} href={href} aria-current={pathname === href || pathname.startsWith(`${href}/`) ? "page" : undefined}><span aria-hidden="true" />{label}</Link>)}</section>)}
        </nav>
        <div className="identity" data-session={identity.status}><span className="identity-orb" aria-hidden="true" /><div><strong>{identity.displayName}</strong><small>{identity.environment} · {identity.organization}</small></div></div>
      </aside>
      <div className="workspace">
        <header className="topbar"><div className="environment" data-session={identity.status}><span aria-hidden="true" />Environment <strong>{identity.environment}</strong></div><button className="icon-button" type="button" disabled title="Notifications require a connected session" aria-label="Notifications unavailable">—</button></header>
        <main id="content" className="content" tabIndex={-1}>{children}</main>
      </div>
    </div>
  );
}
