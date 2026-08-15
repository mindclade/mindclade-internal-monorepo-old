// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Node-agent counters.
use mindclade_telemetry::CounterRegistry;

#[derive(Clone, Debug, Default)]
pub struct NodeMetrics {
    counters: CounterRegistry,
}

impl NodeMetrics {
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
}
