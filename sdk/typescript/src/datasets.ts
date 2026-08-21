// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { components } from "./generated/api.js";
import type { HttpClient } from "./client.js";
import { paginate, type ListOptions } from "./pagination.js";

export type Dataset = components["schemas"]["Dataset"];
export type ListDatasetsResponse = components["schemas"]["ListDatasetsResponse"];

export class DatasetsClient {
  constructor(private readonly http: HttpClient) {}
  list(options: ListOptions = {}): Promise<ListDatasetsResponse> {
    const { signal, ...query } = options;
    return this.http.request({
      path: "/v1/datasets",
      query,
      ...(signal === undefined ? {} : { signal }),
    });
  }
  all(options: Omit<ListOptions, "pageToken"> = {}): AsyncIterable<Dataset> {
    const { signal, ...pageOptions } = options;
    return paginate(
      (page) => this.list({ ...pageOptions, ...page, ...(signal === undefined ? {} : { signal }) }),
      pageOptions,
    );
  }
  get(datasetId: string, signal?: AbortSignal): Promise<Dataset> {
    return this.http.request({ path: `/v1/datasets/${encodeURIComponent(datasetId)}`, ...(signal === undefined ? {} : { signal }) });
  }
}
