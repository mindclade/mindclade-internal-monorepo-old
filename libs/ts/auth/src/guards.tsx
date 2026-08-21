// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactNode } from "react";
import type { Session } from "./types.js";

export function hasScope(session: Session, scope: string): boolean {
  return session.scopes.includes(scope) || session.scopes.includes("*");
}

export function hasEveryScope(session: Session, scopes: readonly string[]): boolean {
  return scopes.every((scope) => hasScope(session, scope));
}

export function Can({ session, scope, children, fallback = null }: {
  session: Session;
  scope: string;
  children: ReactNode;
  fallback?: ReactNode;
}): ReactNode {
  return hasScope(session, scope) ? children : fallback;
}
