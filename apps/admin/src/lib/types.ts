// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface ApprovalEvidence {
  subject: string;
  requestedBy: string;
  reason: string;
  evidenceDigest?: string;
  expiresAt?: string;
}

export interface AuditRecord {
  id: string;
  occurredAt: string;
  actor: string;
  action: string;
  resource: string;
  outcome: "allowed" | "denied" | "failed";
}

export interface QuotaValue { name: string; current: number; requested: number; unit: string }
