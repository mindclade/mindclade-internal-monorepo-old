use std::sync::atomic::{
    AtomicU64, Ordering
};

#[derive(Debug, Default)]
pub struct StreamMetrics {
    shards: AtomicU64, bytes: AtomicU64, records: AtomicU64, cache_hits: AtomicU64, retries: AtomicU64
}

impl StreamMetrics {
    pub fn record_shard(&self, bytes: u64, records: u64) {
        self.shards.fetch_add(1, Ordering::Relaxed);
        self.bytes.fetch_add(bytes, Ordering::Relaxed);
        self.records.fetch_add(records, Ordering::Relaxed);
    }
    pub fn record_cache_hit(&self) {
        self.cache_hits.fetch_add(1, Ordering::Relaxed);
    }
    pub fn record_retry(&self) {
        self.retries.fetch_add(1, Ordering::Relaxed);
    }
    #[must_use]pub fn snapshot(&self) -> (u64, u64, u64, u64, u64) {
        (self.shards.load(Ordering::Relaxed), self.bytes.load(Ordering::Relaxed), self.records.load(Ordering::Relaxed),
        self.cache_hits.load(Ordering::Relaxed), self.retries.load(Ordering::Relaxed))
    }
}
