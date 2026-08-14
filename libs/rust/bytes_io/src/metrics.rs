use std::sync::atomic::{
    AtomicU64, Ordering
};

#[derive(Debug, Default)]
pub struct CopyMetrics {
    copied: AtomicU64, operations: AtomicU64
}

impl CopyMetrics {
    pub fn record(&self, bytes: u64) {
        self.copied.fetch_add(bytes, Ordering::Relaxed);
        self.operations.fetch_add(1, Ordering::Relaxed);
    }
    #[must_use]pub fn copied_bytes(&self) -> u64 {
        self.copied.load(Ordering::Relaxed)
    }
    #[must_use]pub fn operations(&self) -> u64 {
        self.operations.load(Ordering::Relaxed)
    }
}
