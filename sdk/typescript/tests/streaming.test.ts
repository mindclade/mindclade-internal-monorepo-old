// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import test from "node:test";
import { parseServerSentEvents, StreamProtocolError } from "../src/index.js";

test("parseServerSentEvents handles chunk boundaries and multiline data", async () => {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode("event: progress\ndata: {\"stage\":"));
      controller.enqueue(encoder.encode("\"train\",\ndata: \"value\": 2}\n\n"));
      controller.close();
    },
  });
  const events = [];
  for await (const event of parseServerSentEvents(stream)) events.push(event);
  assert.deepEqual(events, [{ event: "progress", data: "{\"stage\":\"train\",\n\"value\": 2}" }]);
});

test("parseServerSentEvents applies the byte limit per event", async () => {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode("data: 1\n\ndata: 2\n\n"));
      controller.close();
    },
  });
  const events = [];
  for await (const event of parseServerSentEvents(stream, { maxEventBytes: 8 })) events.push(event);
  assert.deepEqual(events, [{ data: "1" }, { data: "2" }]);
});

test("parseServerSentEvents rejects an oversized UTF-8 event", async () => {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode("data: 💥\n\n"));
      controller.close();
    },
  });
  await assert.rejects(
    async () => {
      for await (const _event of parseServerSentEvents(stream, { maxEventBytes: 8 })) {
        // The parser must reject before yielding the oversized event.
      }
    },
    (error: unknown) => error instanceof StreamProtocolError,
  );
});

test("parseServerSentEvents cancels the reader when a consumer stops early", async () => {
  const encoder = new TextEncoder();
  let cancelled = false;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode("data: one\n\n"));
    },
    cancel() {
      cancelled = true;
    },
  });
  for await (const _event of parseServerSentEvents(stream)) break;
  assert.equal(cancelled, true);
});
