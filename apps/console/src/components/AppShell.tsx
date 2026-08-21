// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { anonymousIdentity } from "../lib/auth";

const groups = [
  { label: "Operate", items: [["/runs", "Runs"], ["/clusters", "Compute"], ["/serving", "Serving"], ["/rollouts", "Rollouts"]] },
  { label: "Build", items: [["/models", "Models"], ["/datasets", "Datasets"], ["/experiments", "Experiments"], ["/preprocessing", "Preprocessing"]] },
  { label: "Assure", items: [["/evaluations", "Evaluations"], ["/safety", "Safety"], ["/artifacts", "Artifacts"], ["/checkpoints", "Checkpoints"], ["/kernels", "Kernels"]] },
] as const;

export function AppShell({ children }: { children: ReactNode }): ReactNode {
  const pathname = usePathname();
  return (
    <div className="shell">
      <a className="skip-link" href="#content">Skip to content</a>
      <aside className="sidebar">
        <Link className="brand" href="/" aria-label="Mindclade command home"><span className="brand-mark" aria-hidden="true">M</span><span>Mindclade<small>Command</small></span></Link>
        <nav aria-label="Primary navigation">
          {groups.map((group) => <section className="nav-group" key={group.label}><h2>{group.label}</h2>{group.items.map(([href, label]) => <Link key={href} href={href} aria-current={pathname === href || pathname.startsWith(`${href}/`) ? "page" : undefined}><span aria-hidden="true" />{label}</Link>)}</section>)}
        </nav>
        <div className="identity"><span className="identity-orb" aria-hidden="true" /><div><strong>{anonymousIdentity.displayName}</strong><small>{anonymousIdentity.environment} · {anonymousIdentity.organization}</small></div></div>
      </aside>
      <div className="workspace">
        <header className="topbar"><div className="environment"><span aria-hidden="true" />Environment <strong>{anonymousIdentity.environment}</strong></div><button className="icon-button" type="button" disabled title="Notifications require a connected session" aria-label="Notifications unavailable">—</button></header>
        <main id="content" className="content" tabIndex={-1}>{children}</main>
      </div>
    </div>
  );
}
