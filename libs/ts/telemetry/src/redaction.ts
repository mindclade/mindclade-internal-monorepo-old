// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { TelemetryEvent, TelemetryValue } from "./events.js";

const SENSITIVE = /token|secret|password|sequence|payload|email|authorization|cookie/i;
const MAX_STRING_LENGTH = 256;

export function redactEvent(value: TelemetryEvent): TelemetryEvent {
  const properties: Record<string, TelemetryValue> = {};
  for (const [key, candidate] of Object.entries(value.properties ?? {})) {
    if (SENSITIVE.test(key)) {
      properties[key] = "[redacted]";
    } else if (typeof candidate === "string") {
      properties[key] = candidate.slice(0, MAX_STRING_LENGTH);
    } else {
      properties[key] = candidate;
    }
  }
  return { ...value, properties };
}
