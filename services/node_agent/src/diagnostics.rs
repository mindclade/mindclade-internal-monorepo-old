// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded diagnostic bundle collected on stage failure or cancellation.
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DiagnosticEntry {
    pub key: String,
    pub value: String,
}
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct DiagnosticBundle {
    pub entries: Vec<DiagnosticEntry>,
}
impl DiagnosticBundle {
    pub fn insert(&mut self, key: impl Into<String>, value: impl Into<String>) -> FaultResult<()> {
        let key = key.into();
        let mut value = value.into();
        if key.is_empty() || key.len() > 128 || self.entries.len() >= 256 {
            return Err(Fault::invalid_argument("diagnostic entry exceeds bounds"));
        }
        value.truncate(16 * 1024);
        self.entries.push(DiagnosticEntry { key, value });
        Ok(())
    }
}
