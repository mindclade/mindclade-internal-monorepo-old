// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { Button, StatusBadge } from "@mindclade/libs-ts-design-system";
import { useState } from "react";
import type { ApprovalEvidence } from "../lib/types";

export function ApprovalGate({ title, risk, evidence, confirmation, onApprove }: { title: string; risk: string; evidence: ApprovalEvidence; confirmation: string; onApprove?: (reason: string) => Promise<void> }): React.ReactNode {
  const [phrase, setPhrase] = useState(""); const [reason, setReason] = useState(""); const [status, setStatus] = useState<"idle" | "working" | "done" | "error">("idle");
  const approve = async (): Promise<void> => { if (onApprove === undefined || phrase !== confirmation || reason.trim().length < 12) return; setStatus("working"); try { await onApprove(reason.trim()); setStatus("done"); } catch { setStatus("error"); } };
  const reset = (): void => { setPhrase(""); setReason(""); setStatus("idle"); };
  return <section className="admin-panel approval-gate"><header><div><span className="admin-eyebrow">Privileged mutation</span><h2>{title}</h2></div><StatusBadge tone="danger">Elevated approval</StatusBadge></header><div className="risk-copy"><strong>Impact</strong><p>{risk}</p></div><dl><div><dt>Subject</dt><dd>{evidence.subject}</dd></div><div><dt>Requested by</dt><dd>{evidence.requestedBy}</dd></div><div><dt>Evidence</dt><dd>{evidence.evidenceDigest ?? "Not attached"}</dd></div></dl><label>Operational reason<textarea value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Minimum 12 characters; this is written to the audit record." /></label><label>Type <code>{confirmation}</code> to continue<input value={phrase} onChange={(event) => setPhrase(event.target.value)} autoComplete="off" /></label>{status === "error" ? <p role="alert" className="form-error">Approval failed; no mutation was recorded.</p> : null}<footer><Button tone="quiet" type="button" onClick={reset}>Cancel</Button><Button tone="danger" type="button" disabled={onApprove === undefined || phrase !== confirmation || reason.trim().length < 12 || status === "working"} onClick={() => void approve()}>{status === "working" ? "Recording…" : status === "done" ? "Approved" : "Approve mutation"}</Button></footer></section>;
}
