// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { NextRequest } from "next/server";
import nextConfig from "../next.config";
import { approvalReady } from "../src/components/ApprovalGate";
import { AdminRequestError, adminRequest } from "../src/lib/api";
import { proxy } from "../src/proxy";

test("admin emits a no-store standalone bundle with isolation headers", async () => {
  assert.equal(nextConfig.output, "standalone");
  const entries = await nextConfig.headers?.();
  const headers = new Map(entries?.[0]?.headers.map(({ key, value }) => [key, value]));
  assert.equal(headers.get("Cache-Control"), "no-store");
  assert.equal(headers.get("Referrer-Policy"), "no-referrer");
  assert.match(headers.get("Content-Security-Policy") ?? "", /object-src 'none'/);
});

test("admin binds no-store HTML rendering to a request nonce", async () => {
  const response = proxy(new NextRequest("https://governance.example.test/releases"));
  const policy = response.headers.get("content-security-policy") ?? "";
  const scriptSource = policy.split("; ").find((directive) => directive.startsWith("script-src ")) ?? "";
  assert.match(scriptSource, /'nonce-[A-Za-z0-9+/]+'/);
  assert.doesNotMatch(scriptSource, /unsafe-inline/);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.equal(response.headers.get("referrer-policy"), "no-referrer");
});

test("approval readiness requires mutation authority, immutable evidence, reason, and exact phrase", () => {
  const valid = { phrase: "PROMOTE", confirmation: "PROMOTE", reason: "Reviewed qualification evidence", evidenceDigest: "sha256:abc", mutationAvailable: true };
  assert.equal(approvalReady(valid), true);
  assert.equal(approvalReady({ phrase: valid.phrase, confirmation: valid.confirmation, reason: valid.reason, mutationAvailable: true }), false);
  assert.equal(approvalReady({ ...valid, mutationAvailable: false }), false);
  assert.equal(approvalReady({ ...valid, phrase: "promote" }), false);
  assert.equal(approvalReady({ ...valid, reason: "too short" }), false);
});

test("adminRequest rejects mutation calls without audit context before fetch", async () => {
  await assert.rejects(adminRequest("/admin/releases", { method: "POST", body: {} }), (error: unknown) => error instanceof AdminRequestError && error.code === "MUTATION_CONTEXT_REQUIRED");
  await assert.rejects(adminRequest("//evil.example/path"), /root-relative/);
});

test("adminRequest sends bounded idempotent mutation context and parses problem details", async () => {
  const originalFetch = globalThis.fetch;
  try {
    let request: Request | undefined;
    globalThis.fetch = async (input, init) => {
      request = new Request(new URL(String(input), "https://admin.example.test"), init);
      return Response.json({ accepted: true });
    };
    assert.deepEqual(await adminRequest("/admin/releases", {
      method: "POST",
      body: { release: "r1" },
      mutation: { idempotencyKey: "release-promote-0001", reason: "Reviewed all release evidence" },
    }), { accepted: true });
    assert.equal(request?.headers.get("idempotency-key"), "release-promote-0001");
    assert.equal(request?.cache, "no-store");

    globalThis.fetch = async () => Response.json({ detail: "Policy denied the promotion", code: "POLICY_DENIED" }, { status: 403, headers: { "x-request-id": "req_01" } });
    await assert.rejects(adminRequest("/admin/releases"), (error: unknown) => error instanceof AdminRequestError && error.code === "POLICY_DENIED" && error.requestId === "req_01");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
