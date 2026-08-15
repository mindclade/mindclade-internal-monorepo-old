// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

pub use crate::DeviceCapability;
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct DeviceId {
    pub vendor: String,
    pub ordinal: u32,
}
impl DeviceId {
    pub fn validate(&self) -> FaultResult<()> {
        if self.vendor.is_empty()
            || self.vendor.len() > 32
            || self.vendor != self.vendor.trim()
            || !self
                .vendor
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-'))
        {
            return Err(Fault::invalid_argument(
                "accelerator device identity is invalid",
            ));
        }
        Ok(())
    }
}
