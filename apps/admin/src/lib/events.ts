// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { TelemetryClient, event } from "@mindclade/libs-ts-telemetry";

let client: TelemetryClient | undefined;
export function captureAdminIntent(action: string, resourceType: string): void {
  const endpoint = process.env.NEXT_PUBLIC_TELEMETRY_ENDPOINT;
  if (endpoint === undefined) return;
  client ??= new TelemetryClient({ endpoint, batchSize: 1 });
  client.capture(event("admin.intent", { action, resourceType }));
}
