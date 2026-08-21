// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface Principal {
  id: string;
  displayName: string;
  email?: string;
  organizationId: string;
}

export interface Session {
  principal: Principal;
  scopes: readonly string[];
  expiresAt: string;
  assuranceLevel: "standard" | "elevated";
}

export type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "authenticated"; session: Session }
  | { status: "error"; error: Error };
