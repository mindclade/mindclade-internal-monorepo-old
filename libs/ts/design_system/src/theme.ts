// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export const statusTone = {
  queued: "neutral",
  preparing: "info",
  running: "running",
  succeeded: "success",
  passed: "success",
  failed: "danger",
  blocked: "warning",
  cancelled: "neutral",
} as const;

export type Density = "comfortable" | "compact";

export function densityAttribute(density: Density): { "data-density": Density } {
  return { "data-density": density };
}
