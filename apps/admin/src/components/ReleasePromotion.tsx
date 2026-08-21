// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ApprovalEvidence } from "../lib/types";
import { ApprovalGate } from "./ApprovalGate";

export function ReleasePromotion({ evidence, onApprove }: { evidence: ApprovalEvidence; onApprove?: (reason: string) => Promise<void> }): React.ReactNode {
  return <ApprovalGate title="Promote model release" risk="Changes the deployment-eligible release pointer. Runtime rollout remains a separate, observable operation." evidence={evidence} confirmation="PROMOTE RELEASE" {...(onApprove === undefined ? {} : { onApprove })} />;
}
