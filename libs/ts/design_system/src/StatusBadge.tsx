// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement, ReactNode } from "react";

export type StatusTone = "neutral" | "info" | "running" | "success" | "warning" | "danger";

export function StatusBadge({ tone = "neutral", children, pulse = false }: {
  tone?: StatusTone;
  children: ReactNode;
  pulse?: boolean;
}): ReactElement {
  return <span className="mc-status" data-tone={tone} data-pulse={pulse || undefined}><span aria-hidden="true" />{children}</span>;
}
