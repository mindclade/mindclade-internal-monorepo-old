// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Weighted byte admission with RAII release.
//!
//! The semaphore is deliberately non-blocking: online request paths shed load
//! when the configured memory envelope is exhausted instead of creating a
//! second, hidden queue of tasks waiting for memory.

use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ByteSemaphoreSnapshot {
    pub capacity_bytes: u64,
    pub used_bytes: u64,
    pub high_water_bytes: u64,
    pub rejections: u64,
}

#[derive(Debug)]
pub struct ByteSemaphore {
    capacity: u64,
    used: AtomicU64,
    high_water: AtomicU64,
    rejections: AtomicU64,
}

impl ByteSemaphore {
    pub fn new(capacity: u64) -> FaultResult<Arc<Self>> {
        if capacity == 0 {
            return Err(Fault::invalid_argument(
                "byte semaphore capacity must be positive",
            ));
        }
        Ok(Arc::new(Self {
            capacity,
            used: AtomicU64::new(0),
            high_water: AtomicU64::new(0),
            rejections: AtomicU64::new(0),
        }))
    }

    pub fn try_acquire(self: &Arc<Self>, bytes: u64) -> FaultResult<BytePermit> {
        let mut current = self.used.load(Ordering::Acquire);
        loop {
            let next = current.checked_add(bytes).ok_or_else(|| {
                Fault::new(Code::OutOfRange, "byte semaphore accounting overflow")
            })?;
            if next > self.capacity {
                self.rejections.fetch_add(1, Ordering::Relaxed);
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "aggregate byte admission limit exceeded",
                )
                .with_context("requested_bytes", bytes)
                .with_context("used_bytes", current)
                .with_context("capacity_bytes", self.capacity));
            }
            match self.used.compare_exchange_weak(
                current,
                next,
                Ordering::AcqRel,
                Ordering::Acquire,
            ) {
                Ok(_) => {
                    self.high_water.fetch_max(next, Ordering::Relaxed);
                    return Ok(BytePermit {
                        semaphore: Arc::clone(self),
                        bytes,
                    });
                }
                Err(observed) => current = observed,
            }
        }
    }

    #[must_use]
    pub fn snapshot(&self) -> ByteSemaphoreSnapshot {
        ByteSemaphoreSnapshot {
            capacity_bytes: self.capacity,
            used_bytes: self.used.load(Ordering::Acquire),
            high_water_bytes: self.high_water.load(Ordering::Relaxed),
            rejections: self.rejections.load(Ordering::Relaxed),
        }
    }
}

#[derive(Debug)]
pub struct BytePermit {
    semaphore: Arc<ByteSemaphore>,
    bytes: u64,
}

impl BytePermit {
    #[must_use]
    pub const fn bytes(&self) -> u64 {
        self.bytes
    }

    pub fn shrink_to(&mut self, bytes: u64) -> FaultResult<()> {
        if bytes > self.bytes {
            return Err(Fault::invalid_argument(
                "byte permit cannot grow while shrinking",
            ));
        }
        let released = self.bytes - bytes;
        self.bytes = bytes;
        self.semaphore.used.fetch_sub(released, Ordering::AcqRel);
        Ok(())
    }

    pub fn release(mut self) {
        self.release_inner();
    }

    fn release_inner(&mut self) {
        if self.bytes != 0 {
            self.semaphore.used.fetch_sub(self.bytes, Ordering::AcqRel);
            self.bytes = 0;
        }
    }
}

impl Drop for BytePermit {
    fn drop(&mut self) {
        self.release_inner();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn admission_is_weighted_and_released_by_drop() {
        let semaphore = ByteSemaphore::new(10).expect("semaphore");
        let mut first = semaphore.try_acquire(7).expect("first permit");
        assert!(semaphore.try_acquire(4).is_err());
        first.shrink_to(3).expect("shrink");
        let second = semaphore.try_acquire(7).expect("second permit");
        assert_eq!(semaphore.snapshot().used_bytes, 10);
        drop(first);
        drop(second);
        assert_eq!(semaphore.snapshot().used_bytes, 0);
        assert_eq!(semaphore.snapshot().high_water_bytes, 10);
        assert_eq!(semaphore.snapshot().rejections, 1);
    }
}
