// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import assert from "node:assert/strict";
import test from "node:test";
import { AuthClient, AuthClientError, hasEveryScope, isSession, SessionStore, sessionIsExpired, type Session } from "../src/index.js";

const session: Session = { principal: { id: "user_01", displayName: "Operator", organizationId: "org_01" }, scopes: ["runs:read"], expiresAt: "2026-08-20T12:00:00Z", assuranceLevel: "standard" };
test("session helpers fail closed", () => {
  assert.equal(sessionIsExpired(session, Date.parse("2026-08-20T12:00:01Z")), true);
  assert.equal(hasEveryScope(session, ["runs:read"]), true);
  assert.equal(hasEveryScope(session, ["runs:read", "runs:write"]), false);
});

test("AuthClient validates session payloads before exposing identity", async () => {
  const valid = new AuthClient({ fetch: async () => Response.json(session) });
  assert.deepEqual(await valid.session(), session);
  const invalid = new AuthClient({ fetch: async () => Response.json({ principal: { displayName: "Forged" } }) });
  await assert.rejects(invalid.session(), (error: unknown) => error instanceof AuthClientError && error.code === "INVALID_SESSION");
  assert.equal(isSession({ ...session, scopes: ["runs:read", 7] }), false);
});

test("AuthClient bounds session responses and validates configured origins", async () => {
  const oversized = new AuthClient({
    maxResponseBytes: 32,
    fetch: async () => Response.json({ ...session, scopes: ["x".repeat(128)] }),
  });
  await assert.rejects(
    oversized.session(),
    (error: unknown) => error instanceof AuthClientError && error.code === "RESPONSE_TOO_LARGE",
  );
  assert.throws(
    () => new AuthClient({ baseUrl: "https://operator:secret@api.example.test" }),
    /without credentials/,
  );
});

test("AuthClient sanitizes login return targets", () => {
  const client = new AuthClient({ baseUrl: "https://api.example.test" });
  assert.equal(new URL(client.loginUrl("//evil.example")).searchParams.get("return_to"), "/");
  assert.equal(new URL(client.loginUrl("/runs/1")).searchParams.get("return_to"), "/runs/1");
});

test("SessionStore refresh fails closed for expired and malformed sessions", async () => {
  const store = new SessionStore();
  await store.refresh(new AuthClient({ fetch: async () => Response.json({ ...session, expiresAt: "2020-01-01T00:00:00Z" }) }));
  assert.equal(store.getSnapshot().status, "anonymous");
  await store.refresh(new AuthClient({ fetch: async () => Response.json({ nope: true }) }));
  assert.equal(store.getSnapshot().status, "error");
});
