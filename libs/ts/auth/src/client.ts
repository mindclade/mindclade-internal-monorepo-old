// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { Session } from "./types.js";

export interface AuthClientOptions {
  baseUrl?: string;
  fetch?: typeof globalThis.fetch;
  timeoutMs?: number;
  maxResponseBytes?: number;
}

export class AuthClientError extends Error {
  readonly status: number;
  readonly code: "INVALID_SESSION" | "NETWORK_ERROR" | "REQUEST_FAILED" | "RESPONSE_TOO_LARGE" | "TIMEOUT";

  constructor(message: string, options: { status?: number; code: AuthClientError["code"]; cause?: unknown }) {
    super(message, { cause: options.cause });
    this.name = "AuthClientError";
    this.status = options.status ?? 0;
    this.code = options.code;
  }
}

export class AuthClient {
  private readonly baseUrl: string;
  private readonly fetcher: typeof globalThis.fetch;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;

  constructor(options: AuthClientOptions = {}) {
    const rawBaseUrl = options.baseUrl ?? "";
    if (rawBaseUrl === "") {
      this.baseUrl = "";
    } else {
      let baseUrl: URL;
      try {
        baseUrl = new URL(rawBaseUrl);
      } catch (cause) {
        throw new TypeError("Authentication baseUrl must be an absolute URL", { cause });
      }
      if ((baseUrl.protocol !== "https:" && baseUrl.protocol !== "http:") ||
        baseUrl.username !== "" || baseUrl.password !== "" || baseUrl.search !== "" || baseUrl.hash !== "") {
        throw new TypeError("Authentication baseUrl must be an HTTP(S) URL without credentials, query, or fragment");
      }
      this.baseUrl = baseUrl.toString().replace(/\/$/, "");
    }
    this.fetcher = options.fetch ?? globalThis.fetch;
    this.timeoutMs = options.timeoutMs ?? 10_000;
    this.maxResponseBytes = options.maxResponseBytes ?? 64 * 1_024;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new RangeError("timeoutMs must be a positive safe integer");
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new RangeError("maxResponseBytes must be a positive safe integer");
  }

  async session(signal?: AbortSignal): Promise<Session | undefined> {
    const response = await this.request("/auth/session", { headers: { accept: "application/json" } }, signal);
    if (response.status === 401) return undefined;
    if (!response.ok) throw new AuthClientError(`Session request failed with status ${response.status}`, { status: response.status, code: "REQUEST_FAILED" });
    let value: unknown;
    try {
      value = await readBoundedJson(response, this.maxResponseBytes);
    } catch (cause) {
      if (cause instanceof AuthClientError) throw cause;
      throw new AuthClientError("Session response was not valid JSON", { status: response.status, code: "INVALID_SESSION", cause });
    }
    if (!isSession(value)) throw new AuthClientError("Session response did not match the expected contract", { status: response.status, code: "INVALID_SESSION" });
    return value;
  }

  async logout(signal?: AbortSignal): Promise<void> {
    const response = await this.request("/auth/logout", {
      method: "POST", headers: { "x-requested-with": "mindclade-console" },
    }, signal);
    if (!response.ok && response.status !== 401) throw new AuthClientError(`Logout failed with status ${response.status}`, { status: response.status, code: "REQUEST_FAILED" });
  }

  loginUrl(returnTo: string): string {
    const origin = typeof globalThis.location === "undefined" ? "http://localhost" : globalThis.location.origin;
    const url = new URL(`${this.baseUrl}/auth/login`, origin);
    url.searchParams.set("return_to", returnTo.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/");
    return url.toString();
  }

  private async request(path: string, init: RequestInit, signal?: AbortSignal): Promise<Response> {
    const controller = new AbortController();
    const onAbort = (): void => controller.abort(signal?.reason);
    if (signal?.aborted) controller.abort(signal.reason);
    else signal?.addEventListener("abort", onAbort, { once: true });
    const timer = setTimeout(() => controller.abort(new AuthClientError("Authentication request timed out", { code: "TIMEOUT" })), this.timeoutMs);
    try {
      return await this.fetcher(`${this.baseUrl}${path}`, {
        ...init,
        credentials: "include",
        signal: controller.signal,
      });
    } catch (cause) {
      if (controller.signal.reason instanceof AuthClientError) throw controller.signal.reason;
      if (signal?.aborted) throw signal.reason ?? cause;
      throw new AuthClientError("Authentication request failed", { code: "NETWORK_ERROR", cause });
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
    }
  }
}

async function readBoundedJson(response: Response, maxBytes: number): Promise<unknown> {
  const declaredLength = response.headers.get("content-length");
  if (declaredLength !== null) {
    const length = Number(declaredLength);
    if (Number.isFinite(length) && length > maxBytes) throw sessionTooLarge(response, maxBytes);
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
        throw sessionTooLarge(response, maxBytes);
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

function sessionTooLarge(response: Response, maxBytes: number): AuthClientError {
  return new AuthClientError(`Session response exceeded the ${maxBytes} byte limit`, {
    status: response.status,
    code: "RESPONSE_TOO_LARGE",
  });
}

export function isSession(value: unknown): value is Session {
  if (typeof value !== "object" || value === null) return false;
  const session = value as Record<string, unknown>;
  if (session.assuranceLevel !== "standard" && session.assuranceLevel !== "elevated") return false;
  if (typeof session.expiresAt !== "string" || !Number.isFinite(Date.parse(session.expiresAt))) return false;
  if (!Array.isArray(session.scopes) || !session.scopes.every((scope) => typeof scope === "string" && scope.length > 0)) return false;
  if (typeof session.principal !== "object" || session.principal === null) return false;
  const principal = session.principal as Record<string, unknown>;
  return typeof principal.id === "string" && principal.id.length > 0 &&
    typeof principal.displayName === "string" && principal.displayName.length > 0 &&
    typeof principal.organizationId === "string" && principal.organizationId.length > 0 &&
    (principal.email === undefined || typeof principal.email === "string");
}
