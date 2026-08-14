//! Bounded counters exported by the artifact proxy core.
use mindclade_telemetry::CounterRegistry;

#[derive(Clone, Debug, Default)]
pub struct ProxyMetrics {
    counters: CounterRegistry
}

impl ProxyMetrics {
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
    #[must_use] pub fn snapshot(&self) -> std::collections::BTreeMap<String, u64> {
        self.counters.snapshot()
    }
}
