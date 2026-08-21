// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { StatusBadge } from "@mindclade/libs-ts-design-system";

export interface SafetyCheck { name: string; result: "pass" | "fail" | "pending"; evidence?: string }

export function SafetyGate({ checks }: { checks: readonly SafetyCheck[] }): React.ReactNode {
  const blocked = checks.some((check) => check.result === "fail");
  return <section className="panel"><div className="panel-heading"><div><h2>Safety gate</h2><p>Independent evidence required before promotion.</p></div><StatusBadge tone={blocked ? "danger" : checks.every((check) => check.result === "pass") ? "success" : "warning"}>{blocked ? "Blocked" : "Reviewing"}</StatusBadge></div><ul className="signal-list">{checks.map((check) => <li key={check.name}><div><strong>{check.name}</strong><small>{check.evidence ?? "Evidence not attached"}</small></div><StatusBadge tone={check.result === "pass" ? "success" : check.result === "fail" ? "danger" : "warning"}>{check.result}</StatusBadge></li>)}</ul></section>;
}
