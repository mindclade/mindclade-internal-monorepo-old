// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded telemetry delivery batches.

use crate::Envelope;
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DeliveryBatch {
    pub envelopes: Vec<Envelope>,
    pub bytes: u64,
}

impl DeliveryBatch {
    pub fn new(envelopes: Vec<Envelope>, maximum_bytes: u64) -> FaultResult<Self> {
        let mut bytes = 0_u64;
        let mut previous_sequence = None;
        for envelope in &envelopes {
            let payload_bytes = u64::try_from(envelope.payload.len()).map_err(|_| {
                Fault::new(Code::OutOfRange, "telemetry payload length exceeds u64")
            })?;
            bytes = bytes
                .checked_add(payload_bytes)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "telemetry batch byte overflow"))?;
            if bytes > maximum_bytes {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "telemetry batch exceeds limit",
                ));
            }
            if previous_sequence.is_some_and(|previous| envelope.sequence <= previous) {
                return Err(Fault::invalid_argument(
                    "telemetry batch sequence is not strictly increasing",
                ));
            }
            previous_sequence = Some(envelope.sequence);
        }
        Ok(Self { envelopes, bytes })
    }
    #[must_use]
    pub fn highest_sequence(&self) -> Option<u64> {
        self.envelopes.last().map(|envelope| envelope.sequence)
    }
}
