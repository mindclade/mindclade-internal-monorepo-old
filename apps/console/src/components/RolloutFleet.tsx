// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { StatusBadge } from "@mindclade/libs-ts-design-system";

export interface RolloutTarget { region: string; version: string; replicas: number; healthy: number }

export function RolloutFleet({ targets }: { targets: readonly RolloutTarget[] }): React.ReactNode {
  return <ul className="signal-list">{targets.map((target) => <li key={target.region}><div><strong>{target.region}</strong><small>{target.version} · {target.healthy}/{target.replicas} replicas</small></div><StatusBadge tone={target.healthy === target.replicas ? "success" : "warning"}>{target.healthy === target.replicas ? "Healthy" : "Converging"}</StatusBadge></li>)}</ul>;
}
