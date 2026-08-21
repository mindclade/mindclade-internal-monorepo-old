// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { Session, SessionState } from "./types.js";

export class SessionStore {
  private state: SessionState = { status: "loading" };
  private readonly listeners = new Set<() => void>();

  getSnapshot = (): SessionState => this.state;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  setSession(session: Session | undefined): void {
    this.state = session === undefined ? { status: "anonymous" } : { status: "authenticated", session };
    this.emit();
  }

  setError(cause: unknown): void {
    this.state = { status: "error", error: cause instanceof Error ? cause : new Error("Session request failed", { cause }) };
    this.emit();
  }

  private emit(): void { for (const listener of this.listeners) listener(); }
}

export function sessionIsExpired(session: Session, now = Date.now()): boolean {
  return Date.parse(session.expiresAt) <= now;
}
