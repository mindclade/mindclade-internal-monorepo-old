// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{MAX_CONTROL_PAYLOAD, Message};
use mindclade_faults::{Code, Fault, FaultResult};

const FRAME_OVERHEAD_BYTES: u64 = 4096;

pub fn encode_control(message: &Message) -> FaultResult<Vec<u8>> {
    message.validate(MAX_CONTROL_PAYLOAD)?;
    let bytes = message.encode()?;
    let encoded = u64::try_from(bytes.len())
        .map_err(|_| Fault::new(Code::OutOfRange, "encoded IPC control frame exceeds u64"))?;
    let maximum = MAX_CONTROL_PAYLOAD
        .get()
        .checked_add(FRAME_OVERHEAD_BYTES)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "IPC frame bound overflow"))?;
    if encoded > maximum {
        return Err(Fault::invalid_argument(
            "encoded control frame exceeds bound",
        ));
    }
    Ok(bytes)
}

pub fn decode_control(bytes: &[u8]) -> FaultResult<Message> {
    Message::decode(bytes, MAX_CONTROL_PAYLOAD)
}
