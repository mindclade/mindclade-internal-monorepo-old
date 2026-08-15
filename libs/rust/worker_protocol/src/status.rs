// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Validation for worker status reports.

pub use crate::{WorkerState, WorkerStatus};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use std::str::FromStr;

pub const MAX_STATUS_OUTPUTS: usize = 4_096;
const MAX_STATUS_MESSAGE_BYTES: usize = 4_096;
const MAX_CLOCK_SKEW_MILLIS: u64 = 60_000;

pub fn validate(status: &WorkerStatus, now: u64, maximum_outputs: usize) -> FaultResult<()> {
    if status.sequence == 0 {
        return Err(Fault::invalid_argument(
            "worker status sequence must be non-zero",
        ));
    }
    let ticket_id = ResourceId::from_str(&status.ticket_id).map_err(|error| {
        Fault::invalid_argument("worker status ticket ID is invalid").with_source(error)
    })?;
    if ticket_id.kind() != "ticket" {
        return Err(Fault::invalid_argument(
            "worker status ticket ID has wrong resource kind",
        ));
    }
    if status.message.len() > MAX_STATUS_MESSAGE_BYTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "worker status message exceeds bound",
        ));
    }
    let maximum_future = now.checked_add(MAX_CLOCK_SKEW_MILLIS).ok_or_else(|| {
        Fault::new(
            Code::OutOfRange,
            "worker status clock-skew window overflows u64",
        )
    })?;
    if status.observed_unix_millis > maximum_future {
        return Err(Fault::invalid_argument(
            "worker status observation is too far in the future",
        ));
    }
    let maximum_outputs = maximum_outputs.min(MAX_STATUS_OUTPUTS);
    if status.outputs.len() > maximum_outputs {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "worker status output descriptor count exceeds bound",
        ));
    }
    for descriptor in &status.outputs {
        descriptor.validate(now)?;
    }
    Ok(())
}
