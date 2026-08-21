// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

import type { TelemetryEvent } from "./events.js";
import { redactEvent } from "./redaction.js";

export interface TelemetryClientOptions {
  endpoint: string;
  fetch?: typeof globalThis.fetch;
  batchSize?: number;
  maxQueueSize?: number;
}

export class TelemetryClient {
  private readonly fetcher: typeof globalThis.fetch;
  private readonly batchSize: number;
  private readonly maxQueueSize: number;
  private queue: TelemetryEvent[] = [];
  private flushing: Promise<void> | undefined;

  constructor(private readonly options: TelemetryClientOptions) {
    this.fetcher = options.fetch ?? globalThis.fetch;
    this.batchSize = Math.max(1, options.batchSize ?? 20);
    this.maxQueueSize = Math.max(this.batchSize, options.maxQueueSize ?? 200);
  }

  capture(value: TelemetryEvent): void {
    this.queue.push(redactEvent(value));
    if (this.queue.length > this.maxQueueSize) this.queue.splice(0, this.queue.length - this.maxQueueSize);
    if (this.queue.length >= this.batchSize) void this.flush();
  }

  flush(): Promise<void> {
    this.flushing ??= this.flushNext().finally(() => { this.flushing = undefined; });
    return this.flushing;
  }

  flushOnUnload(): boolean {
    if (this.queue.length === 0 || typeof navigator === "undefined" || typeof navigator.sendBeacon !== "function") return false;
    const batch = this.queue.splice(0, this.batchSize);
    return navigator.sendBeacon(this.options.endpoint, new Blob([JSON.stringify({ events: batch })], { type: "application/json" }));
  }

  private async flushNext(): Promise<void> {
    const batch = this.queue.splice(0, this.batchSize);
    if (batch.length === 0) return;
    try {
      const response = await this.fetcher(this.options.endpoint, {
        method: "POST", keepalive: true, headers: { "content-type": "application/json" }, body: JSON.stringify({ events: batch }),
      });
      if (!response.ok) throw new Error(`Telemetry endpoint returned ${response.status}`);
    } catch (cause) {
      this.queue = [...batch, ...this.queue].slice(0, this.maxQueueSize);
      throw cause;
    }
    if (this.queue.length >= this.batchSize) await this.flushNext();
  }
}
