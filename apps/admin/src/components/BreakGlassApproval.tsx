// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ApprovalEvidence } from "../lib/types";
import { ApprovalGate } from "./ApprovalGate";

export function BreakGlassApproval({ evidence, onApprove }: { evidence: ApprovalEvidence; onApprove?: (reason: string) => Promise<void> }): React.ReactNode {
  return <ApprovalGate title="Activate emergency access" risk="Creates short-lived elevated credentials, pages security responders, and requires retrospective review." evidence={evidence} confirmation="ACTIVATE BREAK GLASS" {...(onApprove === undefined ? {} : { onApprove })} />;
}
