// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import { MindcladeClient, type ClientOptions } from "@mindclade/sdk-typescript";
import { EventChannel } from "./events.js";
import type { LoadOptions, ResourceState, Unsubscribe } from "./types.js";

export function createApiClient(options: ClientOptions): MindcladeClient {
  return new MindcladeClient(options);
}

export class ResourceStore<T> {
  private state: ResourceState<T> = { status: "idle" };
  private generation = 0;
  private active: AbortController | undefined;
  private readonly changes = new EventChannel<ResourceState<T>>();

  getSnapshot = (): ResourceState<T> => this.state;

  subscribe = (listener: () => void): Unsubscribe => this.changes.subscribe(listener);

  async load(loader: (signal: AbortSignal) => Promise<T>, options: LoadOptions<T> = {}): Promise<void> {
    const generation = ++this.generation;
    this.active?.abort(new DOMException("Resource load superseded", "AbortError"));
    const controller = new AbortController();
    this.active = controller;
    const abort = (): void => controller.abort(options.signal?.reason);
    if (options.signal?.aborted) controller.abort(options.signal.reason);
    else options.signal?.addEventListener("abort", abort, { once: true });
    const previous = this.state.status === "ready" ? this.state.data : undefined;
    this.set(previous === undefined ? { status: "loading" } : { status: "loading", previous });
    try {
      const data = await loader(controller.signal);
      if (generation !== this.generation) return;
      const updatedAt = Date.now();
      this.set(options.isEmpty?.(data) === true ? { status: "empty", updatedAt } : { status: "ready", data, updatedAt });
    } catch (cause) {
      if (controller.signal.aborted || generation !== this.generation) return;
      const error = cause instanceof Error ? cause : new Error("Resource load failed", { cause });
      this.set(previous === undefined ? { status: "error", error } : { status: "error", error, previous });
    } finally {
      options.signal?.removeEventListener("abort", abort);
      if (this.active === controller) this.active = undefined;
    }
  }

  invalidate(): void {
    this.generation += 1;
    this.active?.abort(new DOMException("Resource invalidated", "AbortError"));
    this.active = undefined;
    this.set({ status: "idle" });
  }

  private set(state: ResourceState<T>): void {
    this.state = state;
    this.changes.emit(state);
  }
}
