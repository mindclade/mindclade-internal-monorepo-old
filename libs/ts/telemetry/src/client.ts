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
  timeoutMs?: number;
}

export class TelemetryClient {
  private readonly fetcher: typeof globalThis.fetch;
  private readonly batchSize: number;
  private readonly maxQueueSize: number;
  private readonly timeoutMs: number;
  private queue: TelemetryEvent[] = [];
  private flushing: Promise<void> | undefined;

  constructor(private readonly options: TelemetryClientOptions) {
    if (options.endpoint.trim() === "") throw new TypeError("Telemetry endpoint is required");
    if (/^[a-z][a-z\d+.-]*:/i.test(options.endpoint)) {
      const protocol = new URL(options.endpoint).protocol;
      if (protocol !== "https:" && protocol !== "http:") throw new TypeError("Telemetry endpoint must use http or https");
    } else if (!options.endpoint.startsWith("/")) {
      throw new TypeError("Relative telemetry endpoint must begin with /");
    }
    this.fetcher = options.fetch ?? globalThis.fetch;
    this.batchSize = positiveInteger(options.batchSize ?? 20, "batchSize");
    this.maxQueueSize = positiveInteger(options.maxQueueSize ?? 200, "maxQueueSize");
    if (this.maxQueueSize < this.batchSize) throw new RangeError("maxQueueSize must be greater than or equal to batchSize");
    this.timeoutMs = positiveInteger(options.timeoutMs ?? 5_000, "timeoutMs");
  }

  capture(value: TelemetryEvent): void {
    this.queue.push(redactEvent(value));
    if (this.queue.length > this.maxQueueSize) this.queue.splice(0, this.queue.length - this.maxQueueSize);
    if (this.queue.length >= this.batchSize) void this.flush().catch(() => undefined);
  }

  flush(): Promise<void> {
    this.flushing ??= this.flushNext().finally(() => { this.flushing = undefined; });
    return this.flushing;
  }

  flushOnUnload(): boolean {
    if (this.queue.length === 0 || typeof navigator === "undefined" || typeof navigator.sendBeacon !== "function") return false;
    const batch = this.queue.splice(0, this.batchSize);
    const accepted = navigator.sendBeacon(this.options.endpoint, new Blob([JSON.stringify({ events: batch })], { type: "application/json" }));
    if (!accepted) this.queue = [...batch, ...this.queue].slice(0, this.maxQueueSize);
    return accepted;
  }

  private async flushNext(): Promise<void> {
    const batch = this.queue.splice(0, this.batchSize);
    if (batch.length === 0) return;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const response = await this.fetcher(this.options.endpoint, {
        method: "POST", keepalive: true, headers: { "content-type": "application/json" }, body: JSON.stringify({ events: batch }), signal: controller.signal,
      });
      if (!response.ok) throw new Error(`Telemetry endpoint returned ${response.status}`);
    } catch (cause) {
      this.queue = [...batch, ...this.queue].slice(0, this.maxQueueSize);
      throw cause;
    } finally {
      clearTimeout(timer);
    }
    if (this.queue.length >= this.batchSize) await this.flushNext();
  }
}

function positiveInteger(value: number, name: string): number {
  if (!Number.isSafeInteger(value) || value <= 0) throw new RangeError(`${name} must be a positive safe integer`);
  return value;
}
