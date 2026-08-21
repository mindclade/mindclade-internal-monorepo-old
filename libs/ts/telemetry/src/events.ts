// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export type TelemetryValue = string | number | boolean | null;

export interface TelemetryEvent {
  name: string;
  occurredAt: string;
  sessionId?: string;
  properties?: Readonly<Record<string, TelemetryValue>>;
}

export function event(name: string, properties?: Readonly<Record<string, TelemetryValue>>): TelemetryEvent {
  return { name, occurredAt: new Date().toISOString(), ...(properties === undefined ? {} : { properties }) };
}
