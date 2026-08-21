// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { components } from "./generated/api.js";
import type { HttpClient } from "./client.js";
import { paginate, type ListOptions } from "./pagination.js";

export type Run = components["schemas"]["Run"];
export type RunState = components["schemas"]["RunState"];
export type RunKind = components["schemas"]["RunKind"];
export type CreateRunRequest = components["schemas"]["CreateRunRequest"];
export type ListRunsResponse = components["schemas"]["ListRunsResponse"];
export interface ListRunsOptions extends ListOptions { state?: RunState }

export class RunsClient {
  constructor(private readonly http: HttpClient) {}

  list(options: ListRunsOptions = {}): Promise<ListRunsResponse> {
    const { signal, ...query } = options;
    return this.http.request({
      method: "GET",
      path: "/v1/runs",
      query,
      ...(signal === undefined ? {} : { signal }),
    });
  }

  all(options: Omit<ListRunsOptions, "pageToken"> = {}): AsyncIterable<Run> {
    const { signal, ...pageOptions } = options;
    return paginate(
      (page) => this.list({ ...pageOptions, ...page, ...(signal === undefined ? {} : { signal }) }),
      pageOptions,
    );
  }

  get(runId: string, options: { signal?: AbortSignal } = {}): Promise<Run> {
    return this.http.request({ path: `/v1/runs/${encodeURIComponent(runId)}`, ...options });
  }

  create(request: CreateRunRequest, options: { idempotencyKey: string; signal?: AbortSignal }): Promise<Run> {
    return this.http.request({
      method: "POST", path: "/v1/runs", body: request, idempotencyKey: options.idempotencyKey,
      ...(options.signal === undefined ? {} : { signal: options.signal }),
    });
  }

  cancel(runId: string, options: { idempotencyKey: string; reason?: string; signal?: AbortSignal }): Promise<Run> {
    return this.http.request({
      method: "POST", path: `/v1/runs/${encodeURIComponent(runId)}:cancel`,
      body: options.reason === undefined ? {} : { reason: options.reason }, idempotencyKey: options.idempotencyKey,
      ...(options.signal === undefined ? {} : { signal: options.signal }),
    });
  }
}
