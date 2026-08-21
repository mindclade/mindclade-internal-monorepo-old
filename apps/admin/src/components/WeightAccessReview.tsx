// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ApprovalEvidence } from "../lib/types";
import { ApprovalGate } from "./ApprovalGate";

export function WeightAccessReview({ evidence, onApprove }: { evidence: ApprovalEvidence; onApprove?: (reason: string) => Promise<void> }): React.ReactNode {
  return <ApprovalGate title="Grant model-weight access" risk="Issues a bounded grant to sensitive model artifacts; all reads are identity-bound and auditable." evidence={evidence} confirmation="GRANT WEIGHT ACCESS" {...(onApprove === undefined ? {} : { onApprove })} />;
}
