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
  if (!/^[a-z][a-z0-9_.-]{0,127}$/i.test(name)) throw new TypeError("Telemetry event name must be 1-128 safe characters");
  return { name, occurredAt: new Date().toISOString(), ...(properties === undefined ? {} : { properties }) };
}
