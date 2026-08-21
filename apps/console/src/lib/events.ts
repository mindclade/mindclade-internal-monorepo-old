// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { TelemetryClient, event } from "@mindclade/libs-ts-telemetry";

let telemetry: TelemetryClient | undefined;

export function captureConsoleEvent(name: string, properties?: Readonly<Record<string, string | number | boolean | null>>): void {
  const endpoint = process.env.NEXT_PUBLIC_TELEMETRY_ENDPOINT;
  if (endpoint === undefined) return;
  telemetry ??= new TelemetryClient({ endpoint });
  telemetry.capture(event(name, properties));
}
