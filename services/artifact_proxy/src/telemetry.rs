// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded counters exported by the artifact proxy core.
use mindclade_telemetry::CounterRegistry;

#[derive(Clone, Debug)]
pub struct ProxyMetrics {
    counters: CounterRegistry,
}

impl Default for ProxyMetrics {
    fn default() -> Self {
        let counters = CounterRegistry::default();
        // Publish every series at zero before any traffic, so a scrape can
        // tell "no reads yet" from "the proxy is not instrumented".
        for name in Self::NAMES {
            let _ = counters.register(name);
        }
        Self { counters }
    }
}

impl ProxyMetrics {
    const NAMES: [&'static str; 6] = [
        "artifact_proxy.read_requests",
        "artifact_proxy.read_bytes",
        "artifact_proxy.write_requests",
        "artifact_proxy.write_bytes",
        "artifact_proxy.cache_hits",
        "artifact_proxy.rejected",
    ];
    pub fn read(&self, bytes: u64) {
        let _ = self.counters.add("artifact_proxy.read_requests", 1);
        let _ = self.counters.add("artifact_proxy.read_bytes", bytes);
    }
    pub fn write(&self, bytes: u64) {
        let _ = self.counters.add("artifact_proxy.write_requests", 1);
        let _ = self.counters.add("artifact_proxy.write_bytes", bytes);
    }
    pub fn cache_hit(&self) {
        let _ = self.counters.add("artifact_proxy.cache_hits", 1);
    }
    pub fn rejected(&self) {
        let _ = self.counters.add("artifact_proxy.rejected", 1);
    }
    #[must_use]
    pub fn snapshot(&self) -> std::collections::BTreeMap<String, u64> {
        self.counters.snapshot()
    }

    /// Prometheus text exposition of every counter above.
    ///
    /// The proxy has always kept these counters and has never had a way to
    /// export them: `/metrics` existed on exactly one Rust service. The body
    /// is available here now; serving it is a route this crate's server owns.
    #[must_use]
    pub fn prometheus(&self) -> String {
        self.counters.prometheus_text()
    }
}
