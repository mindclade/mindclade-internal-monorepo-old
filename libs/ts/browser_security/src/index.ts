// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

export interface BrowserSecurityOptions {
  development?: boolean;
  connectEndpoints?: readonly (string | undefined)[];
  cacheControl?: string;
  referrerPolicy?: "no-referrer" | "strict-origin-when-cross-origin";
  nonce?: string;
}

export interface SecurityHeader { key: string; value: string }

export function endpointOrigin(endpoint: string | undefined, development = false): string | undefined {
  if (endpoint === undefined || endpoint.trim() === "" || endpoint.startsWith("/")) return undefined;
  let url: URL;
  try { url = new URL(endpoint); } catch (cause) {
    throw new TypeError("Public browser endpoint must be an absolute URL or root-relative path", { cause });
  }
  if (url.username !== "" || url.password !== "") throw new TypeError("Public browser endpoint must not contain credentials");
  if (url.protocol !== "https:" && url.protocol !== "http:") throw new TypeError("Public browser endpoint must use http or https");
  if (url.protocol === "http:" && !development && !isLoopback(url.hostname)) {
    throw new TypeError("Production browser endpoints must use https");
  }
  return url.origin;
}

export function resolveBrowserBaseUrl(configured: string | undefined, browserOrigin: string, development = false): string {
  const base = configured === undefined || configured.trim() === "" ? browserOrigin : configured;
  const url = new URL(base, browserOrigin);
  endpointOrigin(url.toString(), development);
  return url.toString().replace(/\/$/, "");
}

export function contentSecurityPolicy(options: BrowserSecurityOptions = {}): string {
  const development = options.development === true;
  const nonce = options.nonce;
  if (nonce !== undefined && !/^[A-Za-z0-9+/_-]{16,128}={0,2}$/.test(nonce)) {
    throw new TypeError("CSP nonce must be a 16-128 character base64 value");
  }
  const connect = new Set<string>(["'self'"]);
  for (const endpoint of options.connectEndpoints ?? []) {
    const origin = endpointOrigin(endpoint, development);
    if (origin !== undefined) connect.add(origin);
  }
  if (development) {
    connect.add("ws://localhost:*");
    connect.add("ws://127.0.0.1:*");
  }
  const scriptSource = nonce === undefined
    ? `'self' 'unsafe-inline'${development ? " 'unsafe-eval'" : ""}`
    : `'self' 'nonce-${nonce}' 'strict-dynamic'${development ? " 'unsafe-eval'" : ""}`;
  const styleElementSource = nonce === undefined ? "'self' 'unsafe-inline'" : `'self' 'nonce-${nonce}'`;
  const directives = [
    "default-src 'self'",
    `script-src ${scriptSource}`,
    "script-src-attr 'none'",
    "style-src 'self' 'unsafe-inline'",
    `style-src-elem ${styleElementSource}`,
    "style-src-attr 'unsafe-inline'",
    "img-src 'self' blob: data:",
    "font-src 'self' data:",
    `connect-src ${[...connect].join(" ")}`,
    "worker-src 'self' blob:",
    "manifest-src 'self'",
    "media-src 'self' blob:",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-src 'none'",
    "frame-ancestors 'none'",
    ...(development ? [] : ["upgrade-insecure-requests"]),
  ];
  return `${directives.join("; ")};`;
}

export function browserSecurityHeaders(options: BrowserSecurityOptions = {}): readonly SecurityHeader[] {
  const development = options.development === true;
  return [
    { key: "Content-Security-Policy", value: contentSecurityPolicy(options) },
    { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
    { key: "Cross-Origin-Resource-Policy", value: "same-origin" },
    { key: "Origin-Agent-Cluster", value: "?1" },
    { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=(), usb=()" },
    { key: "Referrer-Policy", value: options.referrerPolicy ?? "strict-origin-when-cross-origin" },
    { key: "X-Content-Type-Options", value: "nosniff" },
    { key: "X-Frame-Options", value: "DENY" },
    ...(options.cacheControl === undefined ? [] : [{ key: "Cache-Control", value: options.cacheControl }]),
    ...(development ? [] : [{ key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains; preload" }]),
  ];
}

function isLoopback(hostname: string): boolean {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "[::1]";
}
