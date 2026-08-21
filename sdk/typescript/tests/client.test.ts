// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { CancelledError, HttpClient, MindcladeClient, MindcladeError } from "../src/index.js";

test("HttpClient adds auth, query, and idempotency headers", async () => {
  let request: Request | undefined;
  const client = new HttpClient({
    baseUrl: "https://api.example.test/",
    accessToken: async () => "secret",
    fetch: async (input, init) => {
      request = new Request(input, init);
      return Response.json({ id: "run_01" });
    },
  });
  await client.request({ path: "/v1/runs", query: { pageSize: 25 }, idempotencyKey: "key-1" });
  assert.equal(request?.url, "https://api.example.test/v1/runs?pageSize=25");
  assert.equal(request?.headers.get("authorization"), "Bearer secret");
  assert.equal(request?.headers.get("idempotency-key"), "key-1");
});

test("HttpClient preserves structured problem details", async () => {
  const client = new HttpClient({
    baseUrl: "https://api.example.test", maxRetries: 0,
    fetch: async () => Response.json({
      type: "about:blank", title: "No run", status: 404, code: "NOT_FOUND", requestId: "req_01",
    }, { status: 404, headers: { "content-type": "application/problem+json" } }),
  });
  await assert.rejects(client.request({ path: "/v1/runs/missing" }), (error: unknown) => {
    assert.ok(error instanceof MindcladeError);
    assert.equal(error.code, "NOT_FOUND");
    assert.equal(error.requestId, "req_01");
    return true;
  });
});

test("collection clients forward an already-aborted caller signal", async () => {
  const controller = new AbortController();
  controller.abort(new Error("route changed"));
  const client = new MindcladeClient({
    baseUrl: "https://api.example.test",
    fetch: async (_input, init) => {
      assert.equal(init?.signal?.aborted, true);
      throw init?.signal?.reason;
    },
  });

  await assert.rejects(
    client.datasets.list({ pageSize: 25, signal: controller.signal }),
    (error: unknown) => error instanceof CancelledError && error.code === "REQUEST_CANCELLED",
  );
});
