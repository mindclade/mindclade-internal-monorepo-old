// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { AuthClient, sessionIsExpired, type SessionState } from "@mindclade/libs-ts-auth";
import { resolveBrowserBaseUrl } from "@mindclade/libs-ts-browser-security";
import { useEffect, useState } from "react";

export interface ConsoleIdentity {
  displayName: string;
  organization: string;
  environment: string;
  status: SessionState["status"];
}

export function useConsoleIdentity(): ConsoleIdentity {
  const [state, setState] = useState<SessionState>({ status: "loading" });
  const environment = process.env.NEXT_PUBLIC_ENVIRONMENT ?? "local";
  useEffect(() => {
    const controller = new AbortController();
    const baseUrl = resolveBrowserBaseUrl(
      process.env.NEXT_PUBLIC_API_BASE_URL,
      window.location.origin,
      process.env.NODE_ENV !== "production",
    );
    const client = new AuthClient({ baseUrl, timeoutMs: 10_000 });
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
      displayName: state.session.principal.displayName,
      organization: state.session.principal.organizationId,
      environment,
      status: state.status,
    };
  }
  return {
    displayName: state.status === "loading" ? "Verifying session" : state.status === "error" ? "Session unavailable" : "Session not connected",
    organization: state.status === "error" ? "Failing closed" : "Identity unavailable",
    environment,
    status: state.status,
  };
}
