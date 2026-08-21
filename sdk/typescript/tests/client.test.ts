// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { CancelledError, HttpClient, MindcladeClient, MindcladeError, paginate, TimeoutError } from "../src/index.js";

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

test("HttpClient retries transient network failures for safe requests", async () => {
  let attempts = 0;
  const client = new HttpClient({
    baseUrl: "https://api.example.test",
    maxRetries: 1,
    fetch: async () => {
      attempts += 1;
      if (attempts === 1) throw new TypeError("socket reset");
      return Response.json({ ok: true });
    },
  });
  assert.deepEqual(await client.request({ path: "/v1/health" }), { ok: true });
  assert.equal(attempts, 2);
});

test("HttpClient uses one deadline across retries", async () => {
  const client = new HttpClient({
    baseUrl: "https://api.example.test",
    maxRetries: 5,
    timeoutMs: 10,
    fetch: async () => { throw new TypeError("offline"); },
  });
  await assert.rejects(client.request({ path: "/v1/runs" }), (error: unknown) => error instanceof TimeoutError);
});

test("HttpClient applies the request deadline while resolving credentials", async () => {
  let fetched = false;
  const client = new HttpClient({
    baseUrl: "https://api.example.test",
    timeoutMs: 10,
    accessToken: async () => await new Promise<string>(() => undefined),
    fetch: async () => {
      fetched = true;
      return Response.json({});
    },
  });
  await assert.rejects(client.request({ path: "/v1/runs" }), (error: unknown) => error instanceof TimeoutError);
  assert.equal(fetched, false);
});

test("HttpClient bounds decoded JSON response bodies", async () => {
  const client = new HttpClient({
    baseUrl: "https://api.example.test",
    maxResponseBytes: 32,
    fetch: async () => Response.json({ payload: "x".repeat(128) }),
  });
  await assert.rejects(
    client.request({ path: "/v1/runs" }),
    (error: unknown) => error instanceof MindcladeError && error.code === "RESPONSE_TOO_LARGE",
  );
});

test("HttpClient does not retry an unsafe request without idempotency", async () => {
  let attempts = 0;
  const client = new HttpClient({
    baseUrl: "https://api.example.test",
    maxRetries: 2,
    fetch: async () => { attempts += 1; throw new TypeError("offline"); },
  });
  await assert.rejects(client.request({ method: "POST", path: "/v1/runs", body: {} }), (error: unknown) => error instanceof MindcladeError && error.code === "NETWORK_ERROR");
  assert.equal(attempts, 1);
});

test("paginate rejects repeated tokens and page explosions", async () => {
  await assert.rejects(async () => {
    for await (const _ of paginate(async () => ({ items: [1], page: { nextPageToken: "same" } }))) {
      // Consume until the cycle is detected.
    }
  }, /repeated page token/);
  await assert.rejects(async () => {
    for await (const _ of paginate(async ({ pageToken }) => ({ items: [1], page: { nextPageToken: `${pageToken ?? "0"}x` } }), { maxPages: 2 })) {
      // Consume until the bound is reached.
    }
  }, /exceeded the 2 page limit/);
});

test("HttpClient validates base URLs and request paths", async () => {
  assert.throws(() => new HttpClient({ baseUrl: "file:///tmp/api" }), /http or https/);
  assert.throws(() => new HttpClient({ baseUrl: "https://user:secret@api.example.test" }), /must not contain credentials/);
  assert.throws(() => new HttpClient({ baseUrl: "https://api.example.test?tenant=wrong" }), /query or fragment/);
  const client = new HttpClient({ baseUrl: "https://api.example.test", fetch: async () => Response.json({}) });
  await assert.rejects(client.request({ path: "v1/runs" }), (error: unknown) => error instanceof MindcladeError && error.code === "INVALID_REQUEST");
});
