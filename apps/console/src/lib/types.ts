// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export type PublicResourceKind = "runs" | "datasets" | "models" | "artifacts" | "evaluations";

export interface ResourceRow {
  id: string;
  name: string;
  kind: string;
  status: string;
  updatedAt: string;
  href?: string;
}

export interface ResourcePageCopy {
  eyebrow: string;
  title: string;
  description: string;
  emptyTitle: string;
  emptyDetail: string;
  action?: string;
}
