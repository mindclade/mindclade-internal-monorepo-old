// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Payload-free, bounded AI Gateway counters.

use mindclade_telemetry::CounterRegistry;

#[derive(Clone, Debug, Default)]
pub struct GatewayMetrics {
    counters: CounterRegistry,
}

impl GatewayMetrics {
    const NAMES: [&'static str; 6] = [
        "accepted",
        "rejected",
        "dispatched",
        "committed",
        "reconciliation_pending",
        "reconciled",
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

    #[must_use]
    pub fn prometheus(&self) -> String {
        let snapshot = self.snapshot();
        let mut output = String::new();
        for name in Self::NAMES {
            let key = format!("ai_gateway.{name}");
            let value = snapshot.get(&key).copied().unwrap_or_default();
            output.push_str("# TYPE mindclade_ai_gateway_");
            output.push_str(name);
            output.push_str("_total counter\n");
            output.push_str("mindclade_ai_gateway_");
            output.push_str(name);
            output.push_str("_total ");
            output.push_str(&value.to_string());
            output.push('\n');
        }
        output
    }
}
