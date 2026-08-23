// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Node-agent counters.
use mindclade_telemetry::CounterRegistry;

#[derive(Clone, Debug)]
pub struct NodeMetrics {
    counters: CounterRegistry,
}

impl Default for NodeMetrics {
    fn default() -> Self {
        let counters = CounterRegistry::default();
        // Stage failures are the series an operator alerts on, and an alert
        // that cannot fire because the series does not exist until the first
        // failure is worse than no alert. Publish all three at zero.
        for name in Self::NAMES {
            let _ = counters.register(name);
        }
        Self { counters }
    }
}

impl NodeMetrics {
    const NAMES: [&'static str; 3] = [
        "node_agent.stage_started",
        "node_agent.stage_completed",
        "node_agent.stage_failed",
    ];
    pub fn stage_started(&self) {
        let _ = self.counters.add("node_agent.stage_started", 1);
    }
    pub fn stage_completed(&self) {
        let _ = self.counters.add("node_agent.stage_completed", 1);
    }
    pub fn stage_failed(&self) {
        let _ = self.counters.add("node_agent.stage_failed", 1);
    }
    #[must_use]
    pub fn snapshot(&self) -> std::collections::BTreeMap<String, u64> {
        self.counters.snapshot()
    }

    /// Prometheus text exposition of every counter above.
    ///
    /// Rendered by the shared registry rather than by a second hand-written
    /// formatter. Serving it is a route this crate's server owns.
    #[must_use]
    pub fn prometheus(&self) -> String {
        self.counters.prometheus_text()
    }
}
