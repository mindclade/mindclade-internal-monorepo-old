//! Validation for bounded worker control commands.

pub use crate::WorkerCommand;
use mindclade_faults::{Code, Fault, FaultResult};

/// Hard safety ceiling even when a caller configures a larger local limit.
pub const MAX_COMMAND_INPUTS: usize = 4_096;
const MAX_OPERATION_BYTES: usize = 256;
const MAX_REASON_BYTES: usize = 1_024;
const MAX_CLOCK_SKEW_MILLIS: u64 = 60_000;

/// Validate command structure without performing cryptographic ticket
/// verification. `WorkerRuntime` performs signed ticket/revocation validation
/// at lease/admission time.
pub fn validate(command: &WorkerCommand, now: u64, maximum_inputs: usize) -> FaultResult<()> {
    if command.sequence() == 0 {
        return Err(Fault::invalid_argument("worker command sequence must be non-zero"));
    }
    let maximum_inputs = maximum_inputs.min(MAX_COMMAND_INPUTS);
    match command {
        WorkerCommand::Start {
            ticket,
            inputs,
            operation,
            ..
        } => {
            ticket.claims.validate_static()?;
            ticket.signature.validate()?;
            if !valid_operation(operation) {
                return Err(Fault::invalid_argument("worker operation is invalid"));
            }
            if inputs.len() > maximum_inputs {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "worker start command exceeds input descriptor bound",
                ));
            }
            for descriptor in inputs {
                descriptor.validate(now)?;
            }
        }
        WorkerCommand::Cancel {
            reason,
            deadline_unix_millis,
            ..
        }
        | WorkerCommand::Drain {
            reason,
            deadline_unix_millis,
            ..
        } => {
            if reason.trim().is_empty() || reason.len() > MAX_REASON_BYTES {
                return Err(Fault::invalid_argument("worker stop reason is invalid"));
            }
            if *deadline_unix_millis <= now {
                return Err(Fault::new(
                    Code::DeadlineExceeded,
                    "worker stop deadline is not in the future",
                ));
            }
        }
        WorkerCommand::Heartbeat {
            requested_at_unix_millis,
            ..
        } => {
            let maximum_future = now.checked_add(MAX_CLOCK_SKEW_MILLIS).ok_or_else(|| {
                Fault::new(Code::OutOfRange, "heartbeat clock-skew window overflows u64")
            })?;
            if *requested_at_unix_millis > maximum_future {
                return Err(Fault::invalid_argument("heartbeat request is too far in the future"));
            }
        }
    }
    Ok(())
}

fn valid_operation(value: &str) -> bool {
    if value.is_empty() || value.len() > MAX_OPERATION_BYTES || value != value.trim() {
        return false;
    }
    value.bytes().all(|byte| {
        byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-' | b'/' | b':')
    })
}
