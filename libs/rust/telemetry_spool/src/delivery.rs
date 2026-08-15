// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{DeliveryBatch, TelemetrySpool};
use mindclade_faults::FaultResult;

pub trait BatchSink {
    fn deliver(&self, batch: &DeliveryBatch) -> FaultResult<()>;
}

pub fn deliver_after(
    spool: &TelemetrySpool,
    after: u64,
    limit: usize,
    maximum_bytes: u64,
    sink: &dyn BatchSink,
) -> FaultResult<Option<u64>> {
    let batch = DeliveryBatch::new(spool.replay_after(after, limit)?, maximum_bytes)?;
    if batch.envelopes.is_empty() {
        return Ok(None);
    }
    let highest = batch.highest_sequence();
    sink.deliver(&batch)?;
    if let Some(seq) = highest {
        spool.acknowledge(seq)?;
    }
    Ok(highest)
}
