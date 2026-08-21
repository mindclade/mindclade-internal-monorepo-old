// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import { browserSecurityHeaders } from "@mindclade/libs-ts-browser-security";
import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

const development = process.env.NODE_ENV !== "production";

export function proxy(request: NextRequest): NextResponse {
  const nonce = btoa(crypto.randomUUID());
  const responseHeaders = browserSecurityHeaders({
    development,
    nonce,
    connectEndpoints: [process.env.NEXT_PUBLIC_API_BASE_URL, process.env.NEXT_PUBLIC_TELEMETRY_ENDPOINT],
  });
  const requestHeaders = new Headers(request.headers);
  const policy = responseHeaders.find(({ key }) => key === "Content-Security-Policy")?.value;
  if (policy === undefined) throw new Error("Browser security policy did not emit CSP");
  requestHeaders.set("content-security-policy", policy);
  requestHeaders.set("x-nonce", nonce);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  for (const { key, value } of responseHeaders) response.headers.set(key, value);
  return response;
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|robots.txt).*)"],
};
