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
    if (Number.isFinite(seconds)) return Math.min(seconds * 1_000, 10_000);
  }
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

export class HttpClient {
  readonly baseUrl: string;
  private readonly tokenProvider?: AccessTokenProvider;
  private readonly fetcher: typeof globalThis.fetch;
  private readonly defaultHeaders: Readonly<Record<string, string>>;
  private readonly timeoutMs: number;
  private readonly maxRetries: number;
  private readonly credentials?: RequestCredentials;

  constructor(options: ClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
    if (options.accessToken !== undefined) this.tokenProvider = options.accessToken;
    this.fetcher = options.fetch ?? globalThis.fetch;
    this.defaultHeaders = options.headers ?? {};
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.maxRetries = Math.max(0, options.maxRetries ?? 2);
    if (options.credentials !== undefined) this.credentials = options.credentials;
  }

  async request<T>(options: RequestOptions): Promise<T> {
    const response = await this.response(options);
    if (response.status === 204) return undefined as T;
    try {
      return await response.json() as T;
    } catch (cause) {
      throw new MindcladeError("The API returned an invalid JSON response", {
        status: response.status, code: "INVALID_RESPONSE", cause,
      });
    }
  }

  async response(options: RequestOptions): Promise<Response> {
    const method = options.method ?? "GET";
    const canRetry = method === "GET" || options.idempotencyKey !== undefined;
    for (let attempt = 0; ; attempt += 1) {
      const controller = new AbortController();
      const timeoutMs = options.timeoutMs ?? this.timeoutMs;
      const timer = setTimeout(() => controller.abort(new TimeoutError(timeoutMs)), timeoutMs);
      const onAbort = (): void => controller.abort(options.signal?.reason);
      if (options.signal?.aborted) controller.abort(options.signal.reason);
      else options.signal?.addEventListener("abort", onAbort, { once: true });
      try {
        const accessToken = await token(this.tokenProvider);
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
        const response = await this.fetcher(url, {
          method,
          headers,
          signal: controller.signal,
          ...(this.credentials === undefined ? {} : { credentials: this.credentials }),
          ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
        });
        if (response.ok) return response;
        if (canRetry && attempt < this.maxRetries && RETRYABLE_STATUS.has(response.status)) {
          await response.body?.cancel().catch(() => undefined);
          await sleep(retryDelay(response, attempt), options.signal);
          continue;
        }
        throw await this.toError(response);
      } catch (cause) {
        if (cause instanceof MindcladeError) throw cause;
        if (options.signal?.aborted) throw new CancelledError(options.signal.reason ?? cause);
        if (controller.signal.aborted && !options.signal?.aborted) {
          throw controller.signal.reason instanceof TimeoutError
            ? controller.signal.reason
            : new TimeoutError(timeoutMs, cause);
        }
        throw new MindcladeError("Network request failed", { code: "NETWORK_ERROR", cause });
      } finally {
        clearTimeout(timer);
        options.signal?.removeEventListener("abort", onAbort);
      }
    }
  }

  private async toError(response: Response): Promise<MindcladeError> {
    let parsed: unknown;
    try { parsed = await response.json(); } catch { parsed = undefined; }
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
