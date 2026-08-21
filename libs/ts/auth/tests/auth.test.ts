// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { hasEveryScope, sessionIsExpired, type Session } from "../src/index.js";

const session: Session = { principal: { id: "user_01", displayName: "Operator", organizationId: "org_01" }, scopes: ["runs:read"], expiresAt: "2026-08-20T12:00:00Z", assuranceLevel: "standard" };
test("session helpers fail closed", () => {
  assert.equal(sessionIsExpired(session, Date.parse("2026-08-20T12:00:01Z")), true);
  assert.equal(hasEveryScope(session, ["runs:read"]), true);
  assert.equal(hasEveryScope(session, ["runs:read", "runs:write"]), false);
});
