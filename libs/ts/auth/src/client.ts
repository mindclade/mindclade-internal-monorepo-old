// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { Session } from "./types.js";

export interface AuthClientOptions {
  baseUrl?: string;
  fetch?: typeof globalThis.fetch;
}

export class AuthClient {
  private readonly baseUrl: string;
  private readonly fetcher: typeof globalThis.fetch;

  constructor(options: AuthClientOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "").replace(/\/$/, "");
    this.fetcher = options.fetch ?? globalThis.fetch;
  }

  async session(signal?: AbortSignal): Promise<Session | undefined> {
    const response = await this.fetcher(`${this.baseUrl}/auth/session`, {
      credentials: "include", headers: { accept: "application/json" }, ...(signal === undefined ? {} : { signal }),
    });
    if (response.status === 401) return undefined;
    if (!response.ok) throw new Error(`Session request failed with status ${response.status}`);
    return await response.json() as Session;
  }

  async logout(signal?: AbortSignal): Promise<void> {
    const response = await this.fetcher(`${this.baseUrl}/auth/logout`, {
      method: "POST", credentials: "include", headers: { "x-requested-with": "mindclade-console" },
      ...(signal === undefined ? {} : { signal }),
    });
    if (!response.ok && response.status !== 401) throw new Error(`Logout failed with status ${response.status}`);
  }

  loginUrl(returnTo: string): string {
    const origin = typeof globalThis.location === "undefined" ? "http://localhost" : globalThis.location.origin;
    const url = new URL(`${this.baseUrl}/auth/login`, origin);
    url.searchParams.set("return_to", returnTo.startsWith("/") ? returnTo : "/");
    return url.toString();
  }
}
