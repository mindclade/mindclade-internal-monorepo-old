// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteSize;
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ClientConfig {
    pub maximum_read_bytes: ByteSize,
    pub maximum_write_bytes: ByteSize,
    pub maximum_list_items: usize,
}

impl Default for ClientConfig {
    fn default() -> Self {
        Self {
            maximum_read_bytes: ByteSize::new(512 * 1024 * 1024),
            maximum_write_bytes: ByteSize::new(1024 * 1024 * 1024),
            maximum_list_items: 10_000,
        }
    }
}

impl ClientConfig {
    pub fn validate(self) -> FaultResult<Self> {
        if self.maximum_read_bytes.get() == 0
            || self.maximum_write_bytes.get() == 0
            || self.maximum_list_items == 0
        {
            return Err(Fault::invalid_argument(
                "object-store client limits must be positive",
            ));
        }
        Ok(self)
    }
}
