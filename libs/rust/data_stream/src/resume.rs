// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{Cursor, StreamPlan};
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ResumePoint {
    pub shard_index: usize,
    pub record_index: u64,
    pub byte_offset: u64,
}

impl ResumePoint {
    pub fn from_cursor(cursor: &Cursor, plan: &StreamPlan) -> FaultResult<Self> {
        cursor.validate_for(plan)?;
        let shard_index = usize::try_from(cursor.shard_index)
            .map_err(|_| Fault::invalid_argument("cursor shard index does not fit platform"))?;
        Ok(Self {
            shard_index,
            record_index: cursor.record_index,
            byte_offset: cursor.byte_offset,
        })
    }
}
