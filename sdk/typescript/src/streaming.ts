// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { StreamProtocolError } from "./errors.js";

export interface ServerSentEvent { event?: string; id?: string; data: string }

export async function* parseServerSentEvents(
  body: ReadableStream<Uint8Array>,
  options: { maxEventBytes?: number } = {},
): AsyncGenerator<ServerSentEvent, void, undefined> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const maxBytes = options.maxEventBytes ?? 1_048_576;
  const encoder = new TextEncoder();
  let buffer = "";
  let completed = false;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value, { stream: !done }).replaceAll("\r\n", "\n");
      let boundary = buffer.indexOf("\n\n");
      while (boundary >= 0) {
        const frame = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        if (encoder.encode(frame).byteLength > maxBytes) {
          throw new StreamProtocolError("SSE event exceeded the configured size limit");
        }
        const event = parseFrame(frame);
        if (event !== undefined) yield event;
        boundary = buffer.indexOf("\n\n");
      }
      if (encoder.encode(buffer).byteLength > maxBytes) {
        throw new StreamProtocolError("SSE event exceeded the configured size limit");
      }
      if (done) {
        completed = true;
        break;
      }
    }
    if (buffer.trim() !== "") {
      const event = parseFrame(buffer);
      if (event !== undefined) yield event;
    }
  } finally {
    if (!completed) await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

function parseFrame(frame: string): ServerSentEvent | undefined {
  const data: string[] = [];
  let event: string | undefined;
  let id: string | undefined;
  for (const line of frame.split("\n")) {
    if (line === "" || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    const value = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
    if (field === "data") data.push(value);
    else if (field === "event") event = value;
    else if (field === "id" && !value.includes("\0")) id = value;
  }
  if (data.length === 0) return undefined;
  return { data: data.join("\n"), ...(event === undefined ? {} : { event }), ...(id === undefined ? {} : { id }) };
}

export async function* parseJsonEventStream<T>(
  body: ReadableStream<Uint8Array>,
  validate: (value: unknown) => value is T,
): AsyncGenerator<T, void, undefined> {
  for await (const event of parseServerSentEvents(body)) {
    let parsed: unknown;
    try { parsed = JSON.parse(event.data); } catch (cause) {
      throw new StreamProtocolError("SSE data was not valid JSON", cause);
    }
    if (!validate(parsed)) throw new StreamProtocolError("SSE data did not match the expected event contract");
    yield parsed;
  }
}
