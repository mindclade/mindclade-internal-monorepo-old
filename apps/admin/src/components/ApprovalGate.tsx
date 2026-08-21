// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { Button, StatusBadge } from "@mindclade/libs-ts-design-system";
import { useRef, useState } from "react";
import type { ApprovalEvidence } from "../lib/types";

export function approvalReady(options: {
  phrase: string;
  confirmation: string;
  reason: string;
  evidenceDigest?: string;
  mutationAvailable: boolean;
  requiresEvidence?: boolean;
}): boolean {
  return options.mutationAvailable && options.phrase === options.confirmation &&
    options.reason.trim().length >= 12 && options.reason.trim().length <= 1_024 &&
    (options.requiresEvidence === false || (options.evidenceDigest?.trim().length ?? 0) > 0);
}

export function ApprovalGate({ title, risk, evidence, confirmation, onApprove, requiresEvidence = true }: { title: string; risk: string; evidence: ApprovalEvidence; confirmation: string; onApprove?: (reason: string) => Promise<void>; requiresEvidence?: boolean }): React.ReactNode {
  const [phrase, setPhrase] = useState(""); const [reason, setReason] = useState(""); const [status, setStatus] = useState<"idle" | "working" | "done" | "error">("idle");
  const inFlight = useRef(false);
  const ready = approvalReady({ phrase, confirmation, reason, mutationAvailable: onApprove !== undefined, requiresEvidence, ...(evidence.evidenceDigest === undefined ? {} : { evidenceDigest: evidence.evidenceDigest }) });
  const approve = async (): Promise<void> => { if (onApprove === undefined || !ready || inFlight.current) return; inFlight.current = true; setStatus("working"); try { await onApprove(reason.trim()); setStatus("done"); } catch { setStatus("error"); } finally { inFlight.current = false; } };
  const reset = (): void => { setPhrase(""); setReason(""); setStatus("idle"); };
  return <section className="admin-panel approval-gate"><header><div><span className="admin-eyebrow">Privileged mutation</span><h2>{title}</h2></div><StatusBadge tone="danger">Elevated approval</StatusBadge></header><div className="risk-copy"><strong>Impact</strong><p>{risk}</p></div><dl><div><dt>Subject</dt><dd>{evidence.subject}</dd></div><div><dt>Requested by</dt><dd>{evidence.requestedBy}</dd></div><div><dt>Evidence</dt><dd>{evidence.evidenceDigest ?? "Not attached"}</dd></div></dl>{requiresEvidence && evidence.evidenceDigest === undefined ? <p role="status" className="form-error">Immutable evidence must be attached before approval can be enabled.</p> : null}<label>Operational reason<textarea value={reason} maxLength={1_024} onChange={(event) => setReason(event.target.value)} placeholder="Minimum 12 characters; this is written to the audit record." /></label><label>Type <code>{confirmation}</code> to continue<input value={phrase} onChange={(event) => setPhrase(event.target.value)} autoComplete="off" spellCheck={false} /></label>{status === "error" ? <p role="alert" className="form-error">Approval failed; no mutation was recorded.</p> : null}{status === "done" ? <p role="status" className="form-success">Approval was recorded once.</p> : null}<footer><Button tone="quiet" type="button" onClick={reset}>Clear</Button><Button tone="danger" type="button" disabled={!ready || status === "working" || status === "done"} onClick={() => void approve()}>{status === "working" ? "Recording…" : status === "done" ? "Approved" : "Approve mutation"}</Button></footer></section>;
}
