// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { CancelledError, MindcladeError, TimeoutError, type Problem } from "./errors.js";

export type AccessTokenProvider = string | (() => string | undefined | Promise<string | undefined>);

export interface ClientOptions {
  baseUrl: string;
  accessToken?: AccessTokenProvider;
  fetch?: typeof globalThis.fetch;
  headers?: Readonly<Record<string, string>>;
  credentials?: RequestCredentials;
  timeoutMs?: number;
  maxRetries?: number;
  maxResponseBytes?: number;
}

export interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  path: string;
  query?: object;
  body?: unknown;
  headers?: Readonly<Record<string, string>>;
  signal?: AbortSignal;
  timeoutMs?: number;
  idempotencyKey?: string;
}

const RETRYABLE_STATUS = new Set([429, 502, 503, 504]);
const MAX_RETRY_DELAY_MS = 10_000;
const DEFAULT_MAX_RESPONSE_BYTES = 8 * 1_024 * 1_024;

function isProblem(value: unknown): value is Problem {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return typeof candidate.title === "string" && typeof candidate.status === "number" &&
    typeof candidate.code === "string" && typeof candidate.requestId === "string";
}

function retryDelay(response: Response, attempt: number): number {
  const retryAfter = response.headers.get("retry-after");
  if (retryAfter !== null) {
    const seconds = Number(retryAfter);
    if (Number.isFinite(seconds) && seconds >= 0) return Math.min(seconds * 1_000, MAX_RETRY_DELAY_MS);
    const date = Date.parse(retryAfter);
    if (Number.isFinite(date)) return Math.min(Math.max(0, date - Date.now()), MAX_RETRY_DELAY_MS);
  }
  return Math.min(250 * 2 ** attempt + Math.random() * 100, 5_000);
}

function networkRetryDelay(attempt: number): number {
  return Math.min(250 * 2 ** attempt + Math.random() * 100, 5_000);
}

async function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const finish = (): void => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    };
    const onAbort = (): void => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      reject(signal?.reason);
    };
    const timer = setTimeout(finish, ms);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

async function token(provider?: AccessTokenProvider): Promise<string | undefined> {
  return typeof provider === "function" ? provider() : provider;
}

async function abortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) throw signal.reason;
  return await new Promise<T>((resolve, reject) => {
    const onAbort = (): void => reject(signal.reason);
    signal.addEventListener("abort", onAbort, { once: true });
    void promise.then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
  });
}

async function readBoundedJson(response: Response, maxBytes: number): Promise<unknown> {
  const declaredLength = response.headers.get("content-length");
  if (declaredLength !== null) {
    const length = Number(declaredLength);
    if (Number.isFinite(length) && length > maxBytes) throw responseTooLarge(response, maxBytes);
  }
  if (response.body === null) return JSON.parse("");

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > maxBytes) {
        await reader.cancel().catch(() => undefined);
        throw responseTooLarge(response, maxBytes);
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  const body = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(body));
}

function responseTooLarge(response: Response, maxBytes: number): MindcladeError {
  return new MindcladeError(`API response exceeded the ${maxBytes} byte limit`, {
    status: response.status,
    code: "RESPONSE_TOO_LARGE",
  });
}

export class HttpClient {
  readonly baseUrl: string;
  private readonly tokenProvider?: AccessTokenProvider;
  private readonly fetcher: typeof globalThis.fetch;
  private readonly defaultHeaders: Readonly<Record<string, string>>;
  private readonly timeoutMs: number;
  private readonly maxRetries: number;
  private readonly maxResponseBytes: number;
  private readonly credentials?: RequestCredentials;

  constructor(options: ClientOptions) {
    let baseUrl: URL;
    try {
      baseUrl = new URL(options.baseUrl);
    } catch (cause) {
      throw new TypeError("Mindclade API baseUrl must be an absolute URL", { cause });
    }
    if (baseUrl.protocol !== "https:" && baseUrl.protocol !== "http:") {
      throw new TypeError("Mindclade API baseUrl must use http or https");
    }
    if (baseUrl.username !== "" || baseUrl.password !== "") {
      throw new TypeError("Mindclade API baseUrl must not contain credentials");
    }
    if (baseUrl.search !== "" || baseUrl.hash !== "") {
      throw new TypeError("Mindclade API baseUrl must not contain a query or fragment");
    }
    this.baseUrl = baseUrl.toString().replace(/\/$/, "");
    if (options.accessToken !== undefined) this.tokenProvider = options.accessToken;
    this.fetcher = options.fetch ?? globalThis.fetch;
    this.defaultHeaders = options.headers ?? {};
    this.timeoutMs = positiveInteger(options.timeoutMs ?? 30_000, "timeoutMs");
    this.maxRetries = nonNegativeInteger(options.maxRetries ?? 2, "maxRetries");
    this.maxResponseBytes = positiveInteger(
      options.maxResponseBytes ?? DEFAULT_MAX_RESPONSE_BYTES,
      "maxResponseBytes",
    );
    if (options.credentials !== undefined) this.credentials = options.credentials;
  }

  async request<T>(options: RequestOptions): Promise<T> {
    const response = await this.response(options);
    if (response.status === 204) return undefined as T;
    try {
      return await readBoundedJson(response, this.maxResponseBytes) as T;
    } catch (cause) {
      if (cause instanceof MindcladeError) throw cause;
      throw new MindcladeError("The API returned an invalid JSON response", {
        status: response.status, code: "INVALID_RESPONSE", cause,
      });
    }
  }

  async response(options: RequestOptions): Promise<Response> {
    const method = options.method ?? "GET";
    const canRetry = method === "GET" || options.idempotencyKey !== undefined;
    const timeoutMs = positiveInteger(options.timeoutMs ?? this.timeoutMs, "timeoutMs");
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(new TimeoutError(timeoutMs)), timeoutMs);
    const onAbort = (): void => controller.abort(options.signal?.reason);
    if (options.signal?.aborted) controller.abort(options.signal.reason);
    else options.signal?.addEventListener("abort", onAbort, { once: true });
    try {
      if (!options.path.startsWith("/")) {
        throw new MindcladeError("Request path must begin with /", { code: "INVALID_REQUEST" });
      }
      for (let attempt = 0; ; attempt += 1) {
        const accessToken = await abortable(token(this.tokenProvider), controller.signal);
        const headers = new Headers(this.defaultHeaders);
        headers.set("accept", "application/json, application/problem+json");
        if (options.body !== undefined) headers.set("content-type", "application/json");
        if (accessToken !== undefined) headers.set("authorization", `Bearer ${accessToken}`);
        if (options.idempotencyKey !== undefined) headers.set("idempotency-key", options.idempotencyKey);
        for (const [name, value] of Object.entries(options.headers ?? {})) headers.set(name, value);

        const url = new URL(`${this.baseUrl}${options.path}`);
        for (const [name, value] of Object.entries(options.query ?? {})) {
          if (value !== undefined) url.searchParams.set(name, String(value));
        }
        let response: Response;
        try {
          response = await this.fetcher(url, {
            method,
            headers,
            signal: controller.signal,
            ...(this.credentials === undefined ? {} : { credentials: this.credentials }),
            ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
          });
        } catch (cause) {
          if (controller.signal.aborted) throw cause;
          if (canRetry && attempt < this.maxRetries) {
            await sleep(networkRetryDelay(attempt), controller.signal);
            continue;
          }
          throw new MindcladeError("Network request failed", { code: "NETWORK_ERROR", cause });
        }
        if (response.ok) return response;
        if (canRetry && attempt < this.maxRetries && RETRYABLE_STATUS.has(response.status)) {
          await response.body?.cancel().catch(() => undefined);
          await sleep(retryDelay(response, attempt), controller.signal);
          continue;
        }
        throw await this.toError(response);
      }
    } catch (cause) {
      if (cause instanceof MindcladeError) throw cause;
      if (options.signal?.aborted) throw new CancelledError(options.signal.reason ?? cause);
      if (controller.signal.aborted) {
        throw controller.signal.reason instanceof TimeoutError
          ? controller.signal.reason
          : new CancelledError(controller.signal.reason ?? cause);
      }
      throw new MindcladeError("Network request failed", { code: "NETWORK_ERROR", cause });
    } finally {
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", onAbort);
    }
  }

  private async toError(response: Response): Promise<MindcladeError> {
    let parsed: unknown;
    try {
      parsed = await readBoundedJson(response, this.maxResponseBytes);
    } catch (cause) {
      if (cause instanceof MindcladeError) throw cause;
      parsed = undefined;
    }
    if (isProblem(parsed)) {
      return new MindcladeError(parsed.detail ?? parsed.title, {
        status: response.status,
        code: parsed.code,
        requestId: parsed.requestId,
        ...(parsed.retryAfterMs === undefined ? {} : { retryAfterMs: parsed.retryAfterMs }),
        problem: parsed,
      });
    }
    const requestId = response.headers.get("x-request-id") ?? undefined;
    return new MindcladeError(`API request failed with status ${response.status}`, {
      status: response.status,
      code: `HTTP_${response.status}`,
      ...(requestId === undefined ? {} : { requestId }),
    });
  }
}

function positiveInteger(value: number, name: string): number {
  if (!Number.isSafeInteger(value) || value <= 0) throw new RangeError(`${name} must be a positive safe integer`);
  return value;
}

function nonNegativeInteger(value: number, name: string): number {
  if (!Number.isSafeInteger(value) || value < 0) throw new RangeError(`${name} must be a non-negative safe integer`);
  return value;
}
