// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { AuthClient, sessionIsExpired, type SessionState } from "@mindclade/libs-ts-auth";
import { useEffect, useState } from "react";

export interface AdminContext {
  environment: string;
  assurance: "standard" | "elevated" | "unverified";
  operator: string;
  status: SessionState["status"];
}

export function useAdminContext(): AdminContext {
  const [state, setState] = useState<SessionState>({ status: "loading" });
  useEffect(() => {
    const controller = new AbortController();
    const client = new AuthClient({ timeoutMs: 8_000 });
    void client.session(controller.signal).then((session) => {
      if (controller.signal.aborted) return;
      setState(session === undefined || sessionIsExpired(session)
        ? { status: "anonymous" }
        : { status: "authenticated", session });
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) setState({ status: "error", error: cause instanceof Error ? cause : new Error("Session request failed", { cause }) });
    });
    return () => controller.abort();
  }, []);

  if (state.status === "authenticated") {
    return {
      environment: process.env.NEXT_PUBLIC_ENVIRONMENT ?? "local",
      assurance: state.session.assuranceLevel,
      operator: state.session.principal.displayName,
      status: state.status,
    };
  }
  return {
    environment: process.env.NEXT_PUBLIC_ENVIRONMENT ?? "local",
    assurance: "unverified",
    operator: state.status === "loading" ? "Verifying operator" : state.status === "error" ? "Identity unavailable" : "No authenticated operator",
    status: state.status,
  };
}

export function requiresElevation(action: string): boolean {
  return /break-glass|weight|promote|revoke|service-account/i.test(action);
}
