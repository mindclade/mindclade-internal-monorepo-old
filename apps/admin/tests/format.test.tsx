// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { shortIdentity } from "../src/lib/format";

test("shortIdentity bounds long identifiers", () => { assert.ok(shortIdentity("x".repeat(80)).length < 40); });
