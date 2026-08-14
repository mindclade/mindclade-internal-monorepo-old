use mindclade_worker_protocol::BufferDescriptor;
use mindclade_faults::FaultResult;

pub fn validate_descriptor(descriptor: &BufferDescriptor, now_unix_millis: u64) -> FaultResult<()> {
    descriptor.validate(now_unix_millis)
}
