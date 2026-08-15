// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{Cursor, PrefetchedShard, StreamPlan};
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Debug)]
pub struct StreamReader {
    plan: StreamPlan,
    cursor: Cursor,
}

impl StreamReader {
    pub fn new(plan: StreamPlan, cursor: Option<Cursor>) -> FaultResult<Self> {
        let cursor = cursor.unwrap_or_else(|| Cursor::start(plan.plan_digest));
        cursor.validate_for(&plan)?;
        Ok(Self { plan, cursor })
    }
    #[must_use]
    pub fn cursor(&self) -> &Cursor {
        &self.cursor
    }
    pub fn accept(&mut self, item: &PrefetchedShard) -> FaultResult<()> {
        let expected = usize::try_from(self.cursor.shard_index)
            .map_err(|_| Fault::invalid_argument("cursor index exceeds platform usize"))?;
        if item.index != expected {
            return Err(Fault::new(
                Code::Conflict,
                "prefetched shard is out of order",
            ));
        }
        self.cursor.shard_index =
            self.cursor.shard_index.checked_add(1).ok_or_else(|| {
                Fault::new(Code::OutOfRange, "stream cursor shard index exhausted")
            })?;
        self.cursor.record_index = 0;
        self.cursor.byte_offset = 0;
        Ok(())
    }
    #[must_use]
    pub fn plan(&self) -> &StreamPlan {
        &self.plan
    }
}
