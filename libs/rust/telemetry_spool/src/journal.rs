// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Durable spool position accounting.
#![forbid(unsafe_code)]

use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct JournalPosition {
    pub acknowledged: u64,
    pub next_sequence: u64,
}

impl JournalPosition {
    /// Number of unacknowledged sequence positions.
    ///
    /// `acknowledged` is the highest durably acknowledged sequence and
    /// `next_sequence` is the next sequence that would be assigned.  The
    /// relation `acknowledged < next_sequence` is therefore an invariant.
    pub fn pending(self) -> FaultResult<u64> {
        let first_pending = self.acknowledged.checked_add(1).ok_or_else(|| {
            Fault::new(
                Code::OutOfRange,
                "telemetry acknowledgement sequence overflow",
            )
        })?;
        self.next_sequence
            .checked_sub(first_pending)
            .ok_or_else(|| Fault::data_loss("telemetry journal position is inconsistent"))
    }
}
