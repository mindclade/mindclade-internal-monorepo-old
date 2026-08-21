// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { components } from "./generated/api.js";

export type Problem = components["schemas"]["Problem"];

export class MindcladeError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;
  readonly retryAfterMs?: number;
  readonly problem?: Problem;

  constructor(message: string, options: {
    status?: number;
    code?: string;
    requestId?: string;
    retryAfterMs?: number;
    problem?: Problem;
    cause?: unknown;
  } = {}) {
    super(message, { cause: options.cause });
    this.name = "MindcladeError";
    this.status = options.status ?? 0;
    this.code = options.code ?? "CLIENT_ERROR";
    if (options.requestId !== undefined) this.requestId = options.requestId;
    if (options.retryAfterMs !== undefined) this.retryAfterMs = options.retryAfterMs;
    if (options.problem !== undefined) this.problem = options.problem;
  }
}

export class TimeoutError extends MindcladeError {
  constructor(timeoutMs: number, cause?: unknown) {
    super(`Request exceeded the ${timeoutMs} ms deadline`, { code: "DEADLINE_EXCEEDED", cause });
    this.name = "TimeoutError";
  }
}

export class CancelledError extends MindcladeError {
  constructor(cause?: unknown) {
    super("Request was cancelled", { code: "REQUEST_CANCELLED", cause });
    this.name = "CancelledError";
  }
}

export class StreamProtocolError extends MindcladeError {
  constructor(message: string, cause?: unknown) {
    super(message, { code: "STREAM_PROTOCOL_ERROR", cause });
    this.name = "StreamProtocolError";
  }
}
