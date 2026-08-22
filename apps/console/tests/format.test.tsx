// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import assert from "node:assert/strict";
import test from "node:test";
import { formatBytes } from "../src/lib/format";

test("formatBytes reports binary units", () => { assert.equal(formatBytes(1_024), "1 KiB"); });
