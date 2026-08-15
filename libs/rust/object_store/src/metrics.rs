// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Lock-free best-effort object-store counters.

use std::sync::atomic::{AtomicU64, Ordering};

#[derive(Debug, Default)]
pub struct StoreMetrics {
    reads: AtomicU64,
    writes: AtomicU64,
    read_bytes: AtomicU64,
    write_bytes: AtomicU64,
    failures: AtomicU64,
}

impl StoreMetrics {
    pub fn record_read(&self, bytes: u64) {
        saturating_increment(&self.reads, 1);
        saturating_increment(&self.read_bytes, bytes);
    }
    pub fn record_write(&self, bytes: u64) {
        saturating_increment(&self.writes, 1);
        saturating_increment(&self.write_bytes, bytes);
    }
    pub fn record_failure(&self) {
        saturating_increment(&self.failures, 1);
    }
    #[must_use]
    pub fn snapshot(&self) -> (u64, u64, u64, u64, u64) {
        (
            self.reads.load(Ordering::Relaxed),
            self.writes.load(Ordering::Relaxed),
            self.read_bytes.load(Ordering::Relaxed),
            self.write_bytes.load(Ordering::Relaxed),
            self.failures.load(Ordering::Relaxed),
        )
    }
}

fn saturating_increment(counter: &AtomicU64, delta: u64) {
    let mut current = counter.load(Ordering::Relaxed);
    loop {
        let next = current.saturating_add(delta);
        match counter.compare_exchange_weak(current, next, Ordering::Relaxed, Ordering::Relaxed) {
            Ok(_) => return,
            Err(observed) => current = observed,
        }
    }
}
