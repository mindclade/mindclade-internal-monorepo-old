// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import assert from "node:assert/strict";
import test from "node:test";
import { histogram } from "../src/index.js";

test("histogram preserves observation count", () => {
  const bins = histogram([0, 1, 1, 2, 4], 4);
  assert.equal(bins.reduce((sum, value) => sum + value, 0), 5);
  assert.equal(bins.length, 4);
});
