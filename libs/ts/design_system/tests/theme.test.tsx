// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import assert from "node:assert/strict";
import test from "node:test";
import { densityAttribute, statusTone } from "../src/index.js";

test("semantic theme mappings are stable", () => {
  assert.equal(statusTone.failed, "danger");
  assert.deepEqual(densityAttribute("compact"), { "data-density": "compact" });
});
