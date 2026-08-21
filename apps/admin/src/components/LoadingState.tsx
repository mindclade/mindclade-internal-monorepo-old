// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export function LoadingState({ label = "Resolving administrative state" }: { label?: string }): React.ReactNode {
  return <div className="admin-loading" role="status"><span aria-hidden="true" /><strong>{label}</strong><small>Verifying session assurance and policy…</small></div>;
}
