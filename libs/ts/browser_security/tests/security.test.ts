// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { browserSecurityHeaders, contentSecurityPolicy, endpointOrigin, resolveBrowserBaseUrl } from "../src/index.js";

test("production policy is fail-closed and upgrades insecure requests", () => {
  const policy = contentSecurityPolicy({ connectEndpoints: ["https://api.mindclade.example/v1"] });
  assert.match(policy, /connect-src 'self' https:\/\/api\.mindclade\.example/);
  assert.match(policy, /frame-ancestors 'none'/);
  assert.match(policy, /upgrade-insecure-requests/);
  assert.doesNotMatch(policy, /unsafe-eval/);
});

test("security headers include process and transport isolation", () => {
  const headers = new Map(browserSecurityHeaders().map(({ key, value }) => [key, value]));
  assert.equal(headers.get("Cross-Origin-Opener-Policy"), "same-origin");
  assert.match(headers.get("Strict-Transport-Security") ?? "", /includeSubDomains/);
  assert.equal(headers.get("X-Frame-Options"), "DENY");
});

test("request nonces remove unsafe inline script execution", () => {
  const nonce = "Y29uc29sZS1yZXF1ZXN0LW5vbmNl";
  const policy = contentSecurityPolicy({ nonce });
  assert.match(policy, new RegExp(`script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`));
  assert.match(policy, /script-src-attr 'none'/);
  assert.doesNotMatch(policy.split("; ").find((directive) => directive.startsWith("script-src ")) ?? "", /unsafe-inline/);
  assert.throws(() => contentSecurityPolicy({ nonce: "short" }), /base64 value/);
});

test("production rejects insecure public endpoints but permits local development", () => {
  assert.throws(() => endpointOrigin("http://api.example.test"), /must use https/);
  assert.equal(endpointOrigin("http://localhost:8080", true), "http://localhost:8080");
  assert.equal(resolveBrowserBaseUrl("/api", "https://console.example.test"), "https://console.example.test/api");
});
