//! Control-plane versus bulk-data IPC enforcement.

use mindclade_bytes_io::ByteSize;
use mindclade_faults::{
    Code, Fault, FaultResult
};
use mindclade_ipc::{
    Message, MAX_CONTROL_PAYLOAD
};
use mindclade_worker_protocol::BufferDescriptor;

pub fn validate_control_message(message: &Message, maximum_payload_bytes: u64) -> FaultResult<()> {
    if maximum_payload_bytes == 0 || maximum_payload_bytes > MAX_CONTROL_PAYLOAD.get() {
        return Err(Fault::invalid_argument("host control payload limit is invalid"));
    }
    let payload_bytes = u64::try_from(message.payload.len())
    .map_err(|_| Fault::new(Code::OutOfRange, "control message payload length exceeds u64"))?;
    if payload_bytes > maximum_payload_bytes {
        return Err(Fault::new(Code::ResourceExhausted, "control message exceeds host limit; a bulk BufferDescriptor is required"));
    }
    message.payload_digest.verify(&message.payload)
}

pub fn validate_bulk_descriptors(descriptors: &[BufferDescriptor], maximum: usize, now_unix_millis: u64) -> FaultResult<()> {
    if descriptors.len() > maximum {
        return Err(Fault::new(Code::ResourceExhausted, "bulk descriptor count exceeds host limit"));
    }
    for descriptor in descriptors {
        descriptor.validate(now_unix_millis)?;
    }
    Ok(())
}

#[must_use]
pub fn maximum_control_payload(maximum_payload_bytes: u64) -> ByteSize {
    ByteSize::new(maximum_payload_bytes.min(MAX_CONTROL_PAYLOAD.get()))
}
