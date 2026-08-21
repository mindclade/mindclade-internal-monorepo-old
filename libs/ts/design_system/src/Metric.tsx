// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ReactElement, ReactNode } from "react";

export function Metric({ label, value, detail, trend }: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  trend?: "up" | "down" | "flat";
}): ReactElement {
  return (
    <div className="mc-metric">
      <span className="mc-metric__label">{label}</span>
      <strong className="mc-metric__value">{value}</strong>
      {detail === undefined ? null : <span className="mc-metric__detail" data-trend={trend}>{detail}</span>}
    </div>
  );
}
