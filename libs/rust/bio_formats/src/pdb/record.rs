// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Lossless PDB line with parsed record name.
use mindclade_faults::{
    Fault, FaultResult
};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PdbRecord {
    pub record_name: String,
    pub line: Vec<u8>
}

impl PdbRecord {
    pub fn validate(&self) -> FaultResult<()> {
        if self.record_name.is_empty() || self.record_name.len() > 6 || self.line.is_empty() || self.line.contains(&b'\n') || self.line.contains(&b'\r') {
            return Err(Fault::invalid_argument("PDB record is invalid"));
        }
        Ok(())
    }
}
