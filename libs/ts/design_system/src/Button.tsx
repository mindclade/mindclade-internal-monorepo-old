// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { ComponentPropsWithoutRef, ReactElement, ReactNode } from "react";

export type ButtonProps = ComponentPropsWithoutRef<"button"> & {
  tone?: "primary" | "secondary" | "quiet" | "danger";
  size?: "small" | "medium";
  icon?: ReactNode;
};

export function Button({ tone = "secondary", size = "medium", icon, className = "", children, ...props }: ButtonProps): ReactElement {
  return (
    <button className={`mc-button mc-button--${tone} mc-button--${size} ${className}`.trim()} {...props}>
      {icon === undefined ? null : <span className="mc-button__icon" aria-hidden="true">{icon}</span>}
      <span>{children}</span>
    </button>
  );
}
