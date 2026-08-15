// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded CAS retention configuration.

use mindclade_faults::{Fault, FaultResult};
use std::time::Duration;

const MAX_GC_GRACE: Duration = Duration::from_hours(8760);
const MAX_DELETES_PER_RUN: usize = 1_000_000;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RetentionPolicy {
    pub garbage_collection_grace: Duration,
    pub maximum_deletes_per_run: usize,
}

impl RetentionPolicy {
    pub fn validate(self) -> FaultResult<Self> {
        if self.garbage_collection_grace.is_zero()
            || self.garbage_collection_grace > MAX_GC_GRACE
            || self.maximum_deletes_per_run == 0
            || self.maximum_deletes_per_run > MAX_DELETES_PER_RUN
        {
            return Err(Fault::invalid_argument("CAS retention policy is invalid"));
        }
        Ok(self)
    }
}
