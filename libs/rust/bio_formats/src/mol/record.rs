// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Lossless MOL record retained for bounded validation and canonical transport.
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MolRecord {
    pub bytes: Vec<u8>,
}

impl MolRecord {
    pub fn validate(&self) -> FaultResult<()> {
        if self.bytes.is_empty() {
            return Err(Fault::invalid_argument("MOL record is empty"));
        }
        if self.bytes.contains(&0) {
            return Err(Fault::invalid_argument("MOL record contains NUL"));
        }
        Ok(())
    }
}
