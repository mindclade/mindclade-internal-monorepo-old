// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { components } from "./generated/api.js";
import type { HttpClient } from "./client.js";
import { paginate, type ListOptions } from "./pagination.js";

export type Artifact = components["schemas"]["Artifact"];
export type ListArtifactsResponse = components["schemas"]["ListArtifactsResponse"];
export interface ListArtifactsOptions extends ListOptions { kind?: string }

export class ArtifactsClient {
  constructor(private readonly http: HttpClient) {}
  list(options: ListArtifactsOptions = {}): Promise<ListArtifactsResponse> {
    const { signal, ...query } = options;
    return this.http.request({
      path: "/v1/artifacts",
      query,
      ...(signal === undefined ? {} : { signal }),
    });
  }
  all(options: Omit<ListArtifactsOptions, "pageToken"> = {}): AsyncIterable<Artifact> {
    const { signal, ...pageOptions } = options;
    return paginate(
      (page) => this.list({ ...pageOptions, ...page, ...(signal === undefined ? {} : { signal }) }),
      pageOptions,
    );
  }
  get(digest: string, signal?: AbortSignal): Promise<Artifact> {
    return this.http.request({ path: `/v1/artifacts/${encodeURIComponent(digest)}`, ...(signal === undefined ? {} : { signal }) });
  }
}
