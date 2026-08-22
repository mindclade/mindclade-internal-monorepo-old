// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import assert from "node:assert/strict";
import test from "node:test";
import { event, redactEvent, TelemetryClient } from "../src/index.js";

test("telemetry redacts sensitive keys and bounds strings", () => {
  const redacted = redactEvent(event("run.open", { token: "secret", label: "x".repeat(300) }));
  assert.equal(redacted.properties?.token, "[redacted]");
  assert.equal(String(redacted.properties?.label).length, 256);
});

test("TelemetryClient bounds options and sends redacted batches", async () => {
  let payload = "";
  const client = new TelemetryClient({ endpoint: "/telemetry", batchSize: 1, maxQueueSize: 2, fetch: async (_input, init) => {
    payload = String(init?.body ?? "");
    return new Response(null, { status: 202 });
  } });
  client.capture(event("admin.intent", { authorization: "bearer secret", action: "review" }));
  await client.flush();
  assert.match(payload, /\[redacted\]/);
  assert.doesNotMatch(payload, /bearer secret/);
  assert.throws(() => new TelemetryClient({ endpoint: "file:///tmp/events" }), /http or https/);
  assert.throws(() => new TelemetryClient({ endpoint: "/events", batchSize: 3, maxQueueSize: 2 }), /greater than or equal/);
});

test("event rejects unsafe or unbounded names", () => {
  assert.throws(() => event("contains spaces"), /safe characters/);
  assert.throws(() => event(`x${"y".repeat(128)}`), /safe characters/);
});
