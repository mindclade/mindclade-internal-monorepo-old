// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { components } from "./generated/api.js";
import type { HttpClient } from "./client.js";
import { MindcladeError } from "./errors.js";
import { parseJsonEventStream } from "./streaming.js";
import type { Run } from "./runs.js";

export type InferenceRequest = components["schemas"]["InferenceRequest"];
export type InferenceResponse = components["schemas"]["InferenceResponse"];
export type InferenceStreamEvent = components["schemas"]["InferenceStreamEvent"];

function isStreamEvent(value: unknown): value is InferenceStreamEvent {
  if (typeof value !== "object" || value === null || !("type" in value)) return false;
  return ["accepted", "progress", "result", "error"].includes(String((value as { type: unknown }).type));
}

export class InferenceClient {
  constructor(private readonly http: HttpClient) {}

  run(request: InferenceRequest, options: { idempotencyKey: string; signal?: AbortSignal }): Promise<InferenceResponse | Run> {
    return this.http.request({
      method: "POST", path: "/v1/inference", body: request, idempotencyKey: options.idempotencyKey,
      ...(options.signal === undefined ? {} : { signal: options.signal }),
    });
  }

  async *stream(
    request: InferenceRequest,
    options: { idempotencyKey: string; signal?: AbortSignal },
  ): AsyncGenerator<InferenceStreamEvent, void, undefined> {
    const response = await this.http.response({
      method: "POST", path: "/v1/inference:stream", body: request,
      idempotencyKey: options.idempotencyKey, headers: { accept: "text/event-stream" },
      ...(options.signal === undefined ? {} : { signal: options.signal }),
    });
    if (response.body === null) throw new MindcladeError("Inference stream had no response body", { code: "EMPTY_STREAM" });
    yield* parseJsonEventStream(response.body, isStreamEvent);
  }
}
