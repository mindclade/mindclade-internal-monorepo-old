// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Payload-free, bounded AI Gateway counters.

use mindclade_telemetry::CounterRegistry;

#[derive(Clone, Debug)]
pub struct GatewayMetrics {
    counters: CounterRegistry,
}

impl Default for GatewayMetrics {
    fn default() -> Self {
        let counters = CounterRegistry::default();
        // Publish every series at zero before any traffic. A counter absent
        // from a scrape is indistinguishable from missing instrumentation, and
        // the first `rate()` across a series that appears mid-window is wrong.
        // The previous hand-rolled renderer got this by iterating a fixed name
        // list at render time; registering does it once, at construction, and
        // leaves the renderer generic. `tests/metrics.rs` asserts every name
        // reaches the exposition, so a failed registration fails a test.
        for name in Self::NAMES {
            let _ = counters.register(name);
        }
        Self { counters }
    }
}

impl GatewayMetrics {
    const NAMES: [&'static str; 6] = [
        "ai_gateway.accepted",
        "ai_gateway.rejected",
        "ai_gateway.dispatched",
        "ai_gateway.committed",
        "ai_gateway.reconciliation_pending",
        "ai_gateway.reconciled",
    ];
    pub fn accepted(&self) {
        let _ = self.counters.add("ai_gateway.accepted", 1);
    }
    pub fn rejected(&self) {
        let _ = self.counters.add("ai_gateway.rejected", 1);
    }
    pub fn dispatched(&self) {
        let _ = self.counters.add("ai_gateway.dispatched", 1);
    }
    pub fn committed(&self) {
        let _ = self.counters.add("ai_gateway.committed", 1);
    }
    pub fn reconciliation_pending(&self) {
        let _ = self.counters.add("ai_gateway.reconciliation_pending", 1);
    }
    pub fn reconciled(&self) {
        let _ = self.counters.add("ai_gateway.reconciled", 1);
    }
    #[must_use]
    pub fn snapshot(&self) -> std::collections::BTreeMap<String, u64> {
        self.counters.snapshot()
    }

    /// Prometheus text exposition, served from `/metrics`.
    ///
    /// This used to be fifteen hand-rolled lines here, and it was the only
    /// Prometheus-shaped output anywhere in the Rust tree. It now delegates to
    /// `CounterRegistry`, which the artifact proxy and node agent render from
    /// as well, so the four services that had no exposition at all share one
    /// implementation with the one that did.
    #[must_use]
    pub fn prometheus(&self) -> String {
        self.counters.prometheus_text()
    }
}
