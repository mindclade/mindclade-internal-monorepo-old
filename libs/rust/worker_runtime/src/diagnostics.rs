//! Immutable worker diagnostic state safe to publish in bounded status records.

use mindclade_faults::{Fault, FaultResult};
use mindclade_worker_protocol::WorkerState;

const MAX_TICKET_ID_BYTES: usize = 256;
const MAX_CANCELLATION_REASON_BYTES: usize = 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DiagnosticSnapshot {
    pub state: WorkerState,
    pub ticket_id: Option<String>,
    pub next_status_sequence: u64,
    pub cancelled: bool,
    pub cancellation_reason: Option<String>,
}

impl DiagnosticSnapshot {
    pub fn validate(&self) -> FaultResult<()> {
        if self.next_status_sequence == 0 {
            return Err(Fault::invalid_argument(
                "worker diagnostic status sequence must be non-zero",
            ));
        }
        if self.ticket_id.as_ref().is_some_and(|id| {
            id.is_empty() || id.len() > MAX_TICKET_ID_BYTES || id.trim() != id
        }) {
            return Err(Fault::invalid_argument(
                "worker diagnostic ticket identity is invalid",
            ));
        }
        if self.cancellation_reason.as_ref().is_some_and(|reason| {
            reason.is_empty()
                || reason.len() > MAX_CANCELLATION_REASON_BYTES
                || reason.trim() != reason
        }) {
            return Err(Fault::invalid_argument(
                "worker diagnostic cancellation reason is invalid",
            ));
        }
        if self.cancelled != self.cancellation_reason.is_some() {
            return Err(Fault::invalid_argument(
                "worker diagnostic cancellation fields are inconsistent",
            ));
        }
        Ok(())
    }
}
