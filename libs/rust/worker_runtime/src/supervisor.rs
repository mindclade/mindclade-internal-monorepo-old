//! Worker lifecycle supervisor used by ticketed stage executors.

use crate::preemption::PreemptionNotice;
use crate::state;
use crate::WorkerRuntime;
use mindclade_faults::FaultResult;
use mindclade_worker_protocol::WorkerState;
use std::sync::Arc;

#[derive(Clone, Debug)]
pub struct WorkerSupervisor {
    runtime: Arc<WorkerRuntime>,
}

impl WorkerSupervisor {
    #[must_use]
    pub fn new(runtime: Arc<WorkerRuntime>) -> Self {
        Self { runtime }
    }
    #[must_use]
    pub fn runtime(&self) -> Arc<WorkerRuntime> {
        self.runtime.clone()
    }
    /// Request graceful shutdown. Running work drains; idle work is cancelled
    /// immediately because there is no admitted stage to preserve.
    pub fn request_shutdown(&self, reason: impl Into<String>) -> FaultResult<()> {
        let reason = reason.into();
        match self.runtime.state() {
            WorkerState::Running => self.runtime.drain(reason),
            WorkerState::Draining | WorkerState::Cancelling => Ok(()),
            phase if state::is_terminal(phase) => Ok(()),
            _ => self.runtime.cancel(reason),
        }
    }
    /// Enter graceful drain for an externally announced preemption deadline.
    pub fn preempt(&self, notice: &PreemptionNotice, now_unix_millis: u64) -> FaultResult<()> {
        notice.validate(now_unix_millis)?;
        match self.runtime.state() {
            WorkerState::Running => self.runtime.drain(notice.reason.clone()),
            WorkerState::Draining => Ok(()),
            phase if state::is_terminal(phase) => Ok(()),
            _ => self.runtime.cancel(notice.reason.clone()),
        }
    }
    /// Forced shutdown used after the drain deadline expires.
    pub fn force_shutdown(&self, reason: impl Into<String>) -> FaultResult<()> {
        self.runtime.cancel(reason)
    }
}
