// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ApprovalEvidence } from "../lib/types";
import { ApprovalGate } from "./ApprovalGate";

export function EvaluationApproval({ evidence, onApprove }: { evidence: ApprovalEvidence; onApprove?: (reason: string) => Promise<void> }): React.ReactNode {
  return <ApprovalGate title="Accept evaluation evidence" risk="Marks the independent gate as accepted for promotion; underlying evidence remains immutable." evidence={evidence} confirmation="ACCEPT EVIDENCE" {...(onApprove === undefined ? {} : { onApprove })} />;
}
