// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! One SDF record excluding the `$$$$` record delimiter.
use mindclade_faults::{
    Fault, FaultResult
};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SdfRecord {
    pub bytes: Vec<u8>
}

impl SdfRecord {
    pub fn validate(&self) -> FaultResult<()> {
        if self.bytes.is_empty() || self.bytes.windows(4).any(|window| window == b"$$$$") {
            return Err(Fault::invalid_argument("SDF record is empty or embeds a delimiter"));
        }
        Ok(())
    }
}
