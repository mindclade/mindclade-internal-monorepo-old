//! Port contract between Rust runtime host and process-isolated Python workers.

use crate::BatchEnvelope;
use mindclade_faults::FaultResult;
use mindclade_worker_protocol::{BufferDescriptor, ExecutionTicket, WorkerStatus};

#[derive(Clone, Debug)]
pub struct HostInvocation {
    pub ticket: ExecutionTicket,
    pub batches: Vec<BatchEnvelope>,
    pub inputs: Vec<BufferDescriptor>,
}

impl HostInvocation {
    pub fn validate(&self, now_unix_millis: u64) -> FaultResult<()> {
        for batch in &self.batches {
            batch.validate()?;
        }
        for input in &self.inputs {
            input.validate(now_unix_millis)?;
        }
        Ok(())
    }
}

pub trait ModelWorkerPort: Send + Sync {
    fn invoke(&self, invocation: HostInvocation) -> FaultResult<WorkerStatus>;
    fn cancel(&self, ticket_id: &str, reason: &str) -> FaultResult<()>;
    fn drain(&self) -> FaultResult<()>;
}
