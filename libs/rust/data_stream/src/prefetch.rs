// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

pub use crate::{PrefetchedShard, Prefetcher};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PrefetchConfig {
    pub depth: usize,
    pub maximum_shard_bytes: u64,
}

impl PrefetchConfig {
    pub fn validate(self) -> mindclade_faults::FaultResult<Self> {
        if self.depth == 0 || self.depth > 1024 || self.maximum_shard_bytes == 0 {
            return Err(mindclade_faults::Fault::invalid_argument(
                "prefetch configuration is invalid",
            ));
        }
        Ok(self)
    }
}
