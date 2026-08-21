// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { components } from "./generated/api.js";
import type { HttpClient } from "./client.js";
import { paginate, type ListOptions } from "./pagination.js";

export type Evaluation = components["schemas"]["Evaluation"];
export type EvaluationMetric = components["schemas"]["EvaluationMetric"];
export type ListEvaluationsResponse = components["schemas"]["ListEvaluationsResponse"];

export class EvaluationsClient {
  constructor(private readonly http: HttpClient) {}
  list(options: ListOptions = {}): Promise<ListEvaluationsResponse> {
    const { signal, ...query } = options;
    return this.http.request({
      path: "/v1/evaluations",
      query,
      ...(signal === undefined ? {} : { signal }),
    });
  }
  all(options: Omit<ListOptions, "pageToken"> = {}): AsyncIterable<Evaluation> {
    const { signal, ...pageOptions } = options;
    return paginate(
      (page) => this.list({ ...pageOptions, ...page, ...(signal === undefined ? {} : { signal }) }),
      pageOptions,
    );
  }
  get(evaluationId: string, signal?: AbortSignal): Promise<Evaluation> {
    return this.http.request({ path: `/v1/evaluations/${encodeURIComponent(evaluationId)}`, ...(signal === undefined ? {} : { signal }) });
  }
}
