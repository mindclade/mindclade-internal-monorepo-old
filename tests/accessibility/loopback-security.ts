// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

const accessibilityOrigins = new Set([
  "http://127.0.0.1:4411",
  "http://127.0.0.1:4412",
]);

export function loopbackDocumentHeaders(
  requestUrl: string,
  responseHeaders: Readonly<Record<string, string>>,
): Record<string, string> {
  const url = new URL(requestUrl);
  if (!accessibilityOrigins.has(url.origin)) {
    throw new Error(`Accessibility transport override rejected non-test origin ${url.origin}`);
  }

  const headers = { ...responseHeaders };
  const contentSecurityPolicy = Object.keys(headers).find((name) => name.toLowerCase() === "content-security-policy");
  if (contentSecurityPolicy === undefined) {
    throw new Error("Loopback accessibility response did not carry the production CSP");
  }
  headers[contentSecurityPolicy] = headers[contentSecurityPolicy]
    .split(";")
    .map((directive) => directive.trim())
    .filter((directive) => directive !== "" && directive.toLowerCase() !== "upgrade-insecure-requests")
    .join("; ") + ";";

  const strictTransportSecurity = Object.keys(headers).find((name) => name.toLowerCase() === "strict-transport-security");
  if (strictTransportSecurity !== undefined) delete headers[strictTransportSecurity];
  return headers;
}
