// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface AdminContext {
  environment: string;
  assurance: "standard" | "elevated";
  operator: string;
}

export const adminContext: AdminContext = {
  environment: process.env.NEXT_PUBLIC_ENVIRONMENT ?? "local",
  assurance: "standard",
  operator: "Authenticated operator",
};

export function requiresElevation(action: string): boolean {
  return /break-glass|weight|promote|revoke|service-account/i.test(action);
}
