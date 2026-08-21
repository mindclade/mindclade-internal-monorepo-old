// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export { paginate, type Page, type PageOptions } from "@mindclade/sdk-typescript";

export function boundedPageSize(requested: number | undefined, maximum = 100): number {
  if (requested === undefined) return Math.min(50, maximum);
  if (!Number.isFinite(requested)) return Math.min(50, maximum);
  return Math.max(1, Math.min(Math.trunc(requested), maximum));
}
