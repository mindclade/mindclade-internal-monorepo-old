// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { NextRequest } from "next/server";
import nextConfig from "../next.config";
import { proxy } from "../src/proxy";

test("console emits a standalone bundle with browser isolation headers", async () => {
  assert.equal(nextConfig.output, "standalone");
  const entries = await nextConfig.headers?.();
  const headers = new Map(entries?.[0]?.headers.map(({ key, value }) => [key, value]));
  assert.match(headers.get("Content-Security-Policy") ?? "", /frame-ancestors 'none'/);
  assert.equal(headers.get("Cross-Origin-Opener-Policy"), "same-origin");
  assert.equal(headers.get("X-Content-Type-Options"), "nosniff");
});

test("console binds HTML rendering to a request nonce", async () => {
  const response = proxy(new NextRequest("https://console.example.test/runs"));
  const policy = response.headers.get("content-security-policy") ?? "";
  const scriptSource = policy.split("; ").find((directive) => directive.startsWith("script-src ")) ?? "";
  assert.match(scriptSource, /'nonce-[A-Za-z0-9+/]+'/);
  assert.match(scriptSource, /'strict-dynamic'/);
  assert.doesNotMatch(scriptSource, /unsafe-inline/);
});
