// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { components } from "./generated/api.js";
import type { HttpClient } from "./client.js";
import { paginate, type ListOptions } from "./pagination.js";

export type Model = components["schemas"]["Model"];
export type ListModelsResponse = components["schemas"]["ListModelsResponse"];

export class ModelsClient {
  constructor(private readonly http: HttpClient) {}
  list(options: ListOptions = {}): Promise<ListModelsResponse> {
    const { signal, ...query } = options;
    return this.http.request({
      path: "/v1/models",
      query,
      ...(signal === undefined ? {} : { signal }),
    });
  }
  all(options: Omit<ListOptions, "pageToken"> = {}): AsyncIterable<Model> {
    const { signal, ...pageOptions } = options;
    return paginate(
      (page) => this.list({ ...pageOptions, ...page, ...(signal === undefined ? {} : { signal }) }),
      pageOptions,
    );
  }
  get(modelId: string, signal?: AbortSignal): Promise<Model> {
    return this.http.request({ path: `/v1/models/${encodeURIComponent(modelId)}`, ...(signal === undefined ? {} : { signal }) });
  }
}
