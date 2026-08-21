// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

export interface AdminMutationOptions {
  idempotencyKey: string;
  reason: string;
  signal?: AbortSignal;
}

export async function adminRequest<T>(path: string, options: {
  method?: "GET" | "POST" | "PATCH";
  body?: unknown;
  mutation?: AdminMutationOptions;
  signal?: AbortSignal;
} = {}): Promise<T> {
  const headers = new Headers({ accept: "application/json, application/problem+json" });
  if (options.body !== undefined) headers.set("content-type", "application/json");
  if (options.mutation !== undefined) {
    headers.set("idempotency-key", options.mutation.idempotencyKey);
    headers.set("x-mindclade-reason", options.mutation.reason);
  }
  const signal = options.signal ?? options.mutation?.signal;
  const response = await fetch(path, {
    method: options.method ?? "GET", credentials: "include", headers,
    ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
    ...(signal === undefined ? {} : { signal }),
  });
  if (!response.ok) throw new Error(`Administrative request failed with status ${response.status}`);
  return await response.json() as T;
}
