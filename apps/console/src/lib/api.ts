// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { createApiClient } from "@mindclade/libs-ts-api-client";
import type { MindcladeClient } from "@mindclade/sdk-typescript";
import type { PublicResourceKind, ResourceRow } from "./types";

let client: MindcladeClient | undefined;

export function apiClient(): MindcladeClient {
  if (client !== undefined) return client;
  const configured = process.env.NEXT_PUBLIC_API_BASE_URL;
  const baseUrl = configured ?? (typeof window === "undefined" ? "http://localhost" : window.location.origin);
  client = createApiClient({ baseUrl, credentials: "include", timeoutMs: 15_000 });
  return client;
}

export async function loadResources(kind: PublicResourceKind, signal: AbortSignal): Promise<ResourceRow[]> {
  const api = apiClient();
  if (kind === "runs") {
    const response = await api.runs.list({ pageSize: 50, signal });
    return response.items.map((item) => ({ id: item.id, name: item.name, kind: item.kind, status: item.state, updatedAt: item.updatedAt, href: `/runs/${item.id}` }));
  }
  if (kind === "datasets") {
    const response = await api.datasets.list({ pageSize: 50, signal });
    return response.items.map((item) => ({ id: item.id, name: `${item.name} · ${item.version}`, kind: "Dataset", status: item.status, updatedAt: item.createdAt }));
  }
  if (kind === "models") {
    const response = await api.models.list({ pageSize: 50, signal });
    return response.items.map((item) => ({ id: item.id, name: `${item.name} · ${item.version}`, kind: item.family, status: item.status, updatedAt: item.createdAt }));
  }
  if (kind === "artifacts") {
    const response = await api.artifacts.list({ pageSize: 50, signal });
    return response.items.map((item) => ({ id: item.digest, name: item.digest.slice(0, 20), kind: item.kind, status: item.verificationStatus, updatedAt: item.createdAt }));
  }
  const response = await api.evaluations.list({ pageSize: 50, signal });
  return response.items.map((item) => ({ id: item.id, name: item.suite, kind: item.modelId, status: item.status, updatedAt: item.createdAt, href: `/evaluations/${item.id}` }));
}
