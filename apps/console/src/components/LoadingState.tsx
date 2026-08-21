// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export function LoadingState({ label = "Loading workspace data" }: { label?: string }): React.ReactNode {
  return <div className="loading-state" role="status"><span className="loading-orbit" aria-hidden="true"><i /><i /><i /></span><strong>{label}</strong><small>Resolving canonical state…</small></div>;
}
