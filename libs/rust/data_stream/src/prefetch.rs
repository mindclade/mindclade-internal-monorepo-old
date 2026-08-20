// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

pub use crate::{AsyncPrefetcher, PrefetchedShard, Prefetcher};

use std::time::Duration;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PrefetchConfig {
    pub buffer_capacity: usize,
    pub concurrency: usize,
    pub maximum_shard_bytes: u64,
    pub fetch_timeout: Duration,
}

impl PrefetchConfig {
    pub fn validate(self) -> mindclade_faults::FaultResult<Self> {
        if self.buffer_capacity == 0
            || self.buffer_capacity > 1024
            || self.concurrency == 0
            || self.concurrency > 64
            || self.concurrency > self.buffer_capacity
            || self.maximum_shard_bytes == 0
            || self.fetch_timeout.is_zero()
            || self.fetch_timeout > Duration::from_hours(1)
        {
            return Err(mindclade_faults::Fault::invalid_argument(
                "prefetch configuration is invalid",
            ));
        }
        Ok(self)
    }
}
