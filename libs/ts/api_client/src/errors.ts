// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { MindcladeError } from "@mindclade/sdk-typescript";

export type ErrorKind = "authentication" | "authorization" | "not-found" | "conflict" | "rate-limit" | "transient" | "unknown";

export function classifyError(error: unknown): ErrorKind {
  if (!(error instanceof MindcladeError)) return "unknown";
  if (error.status === 401) return "authentication";
  if (error.status === 403) return "authorization";
  if (error.status === 404) return "not-found";
  if (error.status === 409) return "conflict";
  if (error.status === 429) return "rate-limit";
  if (error.status >= 500 || error.code === "NETWORK_ERROR") return "transient";
  return "unknown";
}

export function displayError(error: unknown): string {
  if (error instanceof MindcladeError) return error.message;
  if (error instanceof Error) return error.message;
  return "An unexpected error interrupted the request.";
}
