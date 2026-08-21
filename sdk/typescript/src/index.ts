// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { ArtifactsClient } from "./artifacts.js";
import { HttpClient, type ClientOptions } from "./client.js";
import { DatasetsClient } from "./datasets.js";
import { EvaluationsClient } from "./evaluations.js";
import { InferenceClient } from "./inference.js";
import { ModelsClient } from "./models.js";
import { RunsClient } from "./runs.js";

export class MindcladeClient {
  readonly http: HttpClient;
  readonly runs: RunsClient;
  readonly datasets: DatasetsClient;
  readonly models: ModelsClient;
  readonly artifacts: ArtifactsClient;
  readonly evaluations: EvaluationsClient;
  readonly inference: InferenceClient;

  constructor(options: ClientOptions) {
    this.http = new HttpClient(options);
    this.runs = new RunsClient(this.http);
    this.datasets = new DatasetsClient(this.http);
    this.models = new ModelsClient(this.http);
    this.artifacts = new ArtifactsClient(this.http);
    this.evaluations = new EvaluationsClient(this.http);
    this.inference = new InferenceClient(this.http);
  }
}

export * from "./artifacts.js";
export * from "./client.js";
export * from "./datasets.js";
export * from "./errors.js";
export * from "./evaluations.js";
export * from "./inference.js";
export * from "./models.js";
export * from "./pagination.js";
export * from "./runs.js";
export * from "./streaming.js";
export type { components, operations, paths } from "./generated/api.js";
