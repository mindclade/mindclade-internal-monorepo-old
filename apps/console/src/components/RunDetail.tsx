// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

"use client";

import { Button } from "@mindclade/libs-ts-design-system";
import type { Run } from "@mindclade/sdk-typescript";
import { useEffect, useState } from "react";
import { apiClient } from "../lib/api";
import { LoadingState } from "./LoadingState";
import { RunStatus } from "./RunStatus";

export function RunDetail({ runId }: { runId: string }): React.ReactNode {
  const [state, setState] = useState<{ status: "loading" } | { status: "ready"; run: Run } | { status: "error"; message: string }>({ status: "loading" });
  useEffect(() => {
    const controller = new AbortController();
    apiClient().runs.get(runId, { signal: controller.signal }).then((run) => setState({ status: "ready", run })).catch((cause: unknown) => {
      if (!controller.signal.aborted) setState({ status: "error", message: cause instanceof Error ? cause.message : "Run request failed" });
    });
    return () => controller.abort();
  }, [runId]);
  if (state.status === "loading") return <LoadingState label="Loading run state" />;
  if (state.status === "error") return <div className="state-message state-message--error"><span>Run unavailable</span><h2>Canonical state could not be loaded.</h2><p>{state.message}</p><Button onClick={() => location.reload()}>Retry</Button></div>;
  return <RunStatus run={state.run} />;
}
