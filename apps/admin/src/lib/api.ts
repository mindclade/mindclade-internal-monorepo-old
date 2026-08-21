// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface AdminMutationOptions {
  idempotencyKey: string;
  reason: string;
  signal?: AbortSignal;
}

export class AdminRequestError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;

  constructor(message: string, options: { status?: number; code: string; requestId?: string; cause?: unknown }) {
    super(message, { cause: options.cause });
    this.name = "AdminRequestError";
    this.status = options.status ?? 0;
    this.code = options.code;
    if (options.requestId !== undefined) this.requestId = options.requestId;
  }
}

const MAX_RESPONSE_BYTES = 1_048_576;

export async function adminRequest<T>(path: string, options: {
  method?: "GET" | "POST" | "PATCH";
  body?: unknown;
  mutation?: AdminMutationOptions;
  signal?: AbortSignal;
  timeoutMs?: number;
} = {}): Promise<T> {
  if (!path.startsWith("/") || path.startsWith("//")) throw new TypeError("Administrative request path must be root-relative");
  const method = options.method ?? "GET";
  if (method !== "GET" && options.mutation === undefined) {
    throw new AdminRequestError("Administrative mutations require idempotency and audit reason", { code: "MUTATION_CONTEXT_REQUIRED" });
  }
  const headers = new Headers({ accept: "application/json, application/problem+json" });
  if (options.body !== undefined) headers.set("content-type", "application/json");
  if (options.mutation !== undefined) {
    const idempotencyKey = options.mutation.idempotencyKey.trim();
    const reason = options.mutation.reason.trim();
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$/.test(idempotencyKey)) throw new TypeError("Administrative idempotency key must be 16-128 safe characters");
    if (reason.length < 12 || reason.length > 1_024) throw new TypeError("Administrative reason must contain 12-1024 characters");
    headers.set("idempotency-key", idempotencyKey);
    headers.set("x-mindclade-reason", reason);
  }
  const externalSignal = options.signal ?? options.mutation?.signal;
  const timeoutMs = options.timeoutMs ?? 10_000;
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0) throw new RangeError("timeoutMs must be a positive safe integer");
  const controller = new AbortController();
  const onAbort = (): void => controller.abort(externalSignal?.reason);
  if (externalSignal?.aborted) controller.abort(externalSignal.reason);
  else externalSignal?.addEventListener("abort", onAbort, { once: true });
  const timer = setTimeout(() => controller.abort(new AdminRequestError("Administrative request timed out", { code: "DEADLINE_EXCEEDED" })), timeoutMs);
  try {
    const response = await fetch(path, {
      method,
      credentials: "include",
      headers,
      cache: "no-store",
      redirect: "error",
      referrerPolicy: "no-referrer",
      signal: controller.signal,
      ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
    });
    const requestId = response.headers.get("x-request-id") ?? undefined;
    const text = await boundedResponseText(response, MAX_RESPONSE_BYTES);
    if (!response.ok) {
      const problem = parseRecord(text);
      const message = typeof problem?.detail === "string" ? problem.detail : `Administrative request failed with status ${response.status}`;
      const code = typeof problem?.code === "string" ? problem.code : `HTTP_${response.status}`;
      throw new AdminRequestError(message, { status: response.status, code, ...(requestId === undefined ? {} : { requestId }) });
    }
    if (response.status === 204 || text === "") return undefined as T;
    try { return JSON.parse(text) as T; } catch (cause) {
      throw new AdminRequestError("Administrative response was not valid JSON", { status: response.status, code: "INVALID_RESPONSE", ...(requestId === undefined ? {} : { requestId }), cause });
    }
  } catch (cause) {
    if (cause instanceof AdminRequestError) throw cause;
    if (controller.signal.reason instanceof AdminRequestError) throw controller.signal.reason;
    if (externalSignal?.aborted) throw new AdminRequestError("Administrative request was cancelled", { code: "REQUEST_CANCELLED", cause: externalSignal.reason ?? cause });
    throw new AdminRequestError("Administrative request failed before a response was received", { code: "NETWORK_ERROR", cause });
  } finally {
    clearTimeout(timer);
    externalSignal?.removeEventListener("abort", onAbort);
  }
}

async function boundedResponseText(response: Response, maxBytes: number): Promise<string> {
  const declared = Number(response.headers.get("content-length") ?? 0);
  if (Number.isFinite(declared) && declared > maxBytes) {
    await response.body?.cancel().catch(() => undefined);
    throw new AdminRequestError("Administrative response exceeded the size limit", { status: response.status, code: "RESPONSE_TOO_LARGE" });
  }
  if (response.body === null) return "";
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let size = 0;
  let text = "";
  let completed = false;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        text += decoder.decode();
        completed = true;
        return text;
      }
      size += value.byteLength;
      if (size > maxBytes) throw new AdminRequestError("Administrative response exceeded the size limit", { status: response.status, code: "RESPONSE_TOO_LARGE" });
      text += decoder.decode(value, { stream: true });
    }
  } finally {
    if (!completed) await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

function parseRecord(text: string): Record<string, unknown> | undefined {
  try {
    const value: unknown = JSON.parse(text);
    return typeof value === "object" && value !== null ? value as Record<string, unknown> : undefined;
  } catch { return undefined; }
}
