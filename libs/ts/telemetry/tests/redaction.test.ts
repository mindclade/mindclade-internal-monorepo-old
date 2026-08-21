// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { event, redactEvent } from "../src/index.js";

test("telemetry redacts sensitive keys and bounds strings", () => {
  const redacted = redactEvent(event("run.open", { token: "secret", label: "x".repeat(300) }));
  assert.equal(redacted.properties?.token, "[redacted]");
  assert.equal(String(redacted.properties?.label).length, 256);
});
