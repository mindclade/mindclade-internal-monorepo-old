// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { Button } from "@mindclade/libs-ts-design-system";
import { useState } from "react";
import type { QuotaValue } from "../lib/types";

export function QuotaEditor({ values, onSubmit }: { values: readonly QuotaValue[]; onSubmit?: (values: readonly QuotaValue[], reason: string) => Promise<void> }): React.ReactNode {
  const [draft, setDraft] = useState(() => values.map((value) => ({ ...value })));
  const [reason, setReason] = useState("");
  return <section className="admin-panel quota-editor"><header><div><span className="admin-eyebrow">Capacity policy</span><h2>Quota change set</h2></div></header>{draft.map((quota, index) => <label key={quota.name}><span><strong>{quota.name}</strong><small>Current {quota.current.toLocaleString()} {quota.unit}</small></span><input type="number" min="0" value={quota.requested} onChange={(event) => setDraft((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, requested: Number(event.target.value) } : item))} /><em>{quota.unit}</em></label>)}<label className="quota-reason"><span><strong>Reason</strong><small>Required in audit record</small></span><textarea value={reason} onChange={(event) => setReason(event.target.value)} /></label><footer><Button disabled={onSubmit === undefined || reason.trim().length < 12} onClick={() => onSubmit === undefined ? undefined : void onSubmit(draft, reason)}>Review change set</Button></footer></section>;
}
