// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Hard limits for the artifact proxy.
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProxyConfig {
    pub maximum_read_bytes: u64,
    pub maximum_write_bytes: u64,
    pub maximum_range_bytes: u64,
    pub maximum_cache_bytes: u64,
    pub maximum_cache_entries: usize,
    pub maximum_concurrent_transfers: u32,
}
impl ProxyConfig {
    pub fn validate(&self) -> FaultResult<()> {
        if self.maximum_read_bytes == 0
            || self.maximum_write_bytes == 0
            || self.maximum_range_bytes == 0
            || self.maximum_range_bytes > self.maximum_read_bytes
            || self.maximum_cache_entries == 0
            || self.maximum_cache_entries > 1_000_000
            || self.maximum_concurrent_transfers == 0
        {
            return Err(Fault::invalid_argument("artifact proxy limits are invalid"));
        }
        Ok(())
    }
}
