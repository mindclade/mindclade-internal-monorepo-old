// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"use client";

import { Button } from "@mindclade/libs-ts-design-system";
import type { Evaluation } from "@mindclade/sdk-typescript";
import { useEffect, useState } from "react";
import { apiClient } from "../lib/api";
import { EvaluationSummary } from "./EvaluationSummary";
import { LoadingState } from "./LoadingState";

export function EvaluationDetail({ evaluationId }: { evaluationId: string }): React.ReactNode {
  const [state, setState] = useState<{ status: "loading" } | { status: "ready"; evaluation: Evaluation } | { status: "error"; message: string }>({ status: "loading" });
  useEffect(() => {
    const controller = new AbortController();
    apiClient().evaluations.get(evaluationId, controller.signal).then((evaluation) => setState({ status: "ready", evaluation })).catch((cause: unknown) => {
      if (!controller.signal.aborted) setState({ status: "error", message: cause instanceof Error ? cause.message : "Evaluation request failed" });
    });
    return () => controller.abort();
  }, [evaluationId]);
  if (state.status === "loading") return <LoadingState label="Loading evaluation evidence" />;
  if (state.status === "error") return <div className="state-message state-message--error"><span>Evaluation unavailable</span><h2>Evidence could not be loaded.</h2><p>{state.message}</p><Button onClick={() => location.reload()}>Retry</Button></div>;
  return <EvaluationSummary evaluation={state.evaluation} />;
}
