//! Reusable ticketed worker state machine for ingestion, preprocessing,
//! evaluation, batch inference, checkpoint transfer, and node-local stages.
#![forbid(unsafe_code)]

pub mod commit;
pub mod diagnostics;
pub mod drain;
pub mod heartbeat;
pub mod lease;
pub mod machine;
pub mod preemption;
pub mod state;
pub mod supervisor;
use diagnostics::DiagnosticSnapshot;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::{Budget, CancellationToken, FencingToken, Reservation};
use mindclade_worker_protocol::{
    ExecutionTicket, RevocationSnapshot, SignatureVerifier, WorkerState,
};
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug)]
struct RuntimeState {
    phase: WorkerState,
    ticket: Option<String>,
    fencing: Option<FencingToken>,
    next_status_sequence: u64,
}

/// Process-local execution authority for one ticket at a time.
///
/// `WorkerRuntime` owns the state transition table, cancellation token, fencing
/// generation, and the node-budget reservation acquired for the active ticket.
/// Product-specific scientific/model execution remains outside this crate.
#[derive(Debug)]
pub struct WorkerRuntime {
    state: Mutex<RuntimeState>,
    cancellation: CancellationToken,
    budget: Arc<Budget>,
    reservation: Mutex<Option<Reservation>>,
}

impl WorkerRuntime {
    #[must_use]
    pub fn new(budget: Arc<Budget>) -> Self {
        Self {
            state: Mutex::new(RuntimeState {
                phase: WorkerState::Created,
                ticket: None,
                fencing: None,
                next_status_sequence: 1,
            }),
            cancellation: CancellationToken::new(),
            budget,
            reservation: Mutex::new(None),
        }
    }
    #[must_use]
    pub fn state(&self) -> WorkerState {
        self.state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .phase
    }
    #[must_use]
    pub fn cancellation_token(&self) -> CancellationToken {
        self.cancellation.clone()
    }
    pub fn start(&self) -> FaultResult<()> {
        self.transition(WorkerState::Starting)?;
        self.transition(WorkerState::Ready)
    }
    /// Validate and claim an execution ticket, atomically installing the node
    /// resource reservation before the worker becomes visible as `Leased`.
    pub fn lease<V: SignatureVerifier + ?Sized>(
        &self,
        ticket: &ExecutionTicket,
        now: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<()> {
        ticket.validate(
            now,
            minimum_policy_epoch,
            minimum_route_version,
            revocations,
            verifier,
        )?;
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.phase != WorkerState::Ready {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "worker is not ready for lease",
            ));
        }
        let reservation = self.budget.reserve(ticket.claims.budget.to_resources())?;
        let mut slot = self
            .reservation
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if slot.is_some() {
            return Err(Fault::new(
                Code::Internal,
                "worker resource reservation was already occupied",
            ));
        }
        *slot = Some(reservation);
        state.phase = WorkerState::Leased;
        state.ticket = Some(ticket.claims.ticket_id.to_string());
        state.fencing = Some(ticket.claims.fencing_token);
        Ok(())
    }
    pub fn run(&self) -> FaultResult<()> {
        self.transition(WorkerState::Running)
    }
    /// Graceful drain: stop progressing into new work but allow the currently
    /// running stage to reach a safe commit point. This deliberately does not
    /// trip the cancellation token.
    pub fn drain(&self, _reason: impl Into<String>) -> FaultResult<()> {
        let phase = self.state();
        if state::is_terminal(phase) || phase == WorkerState::Draining {
            return Ok(());
        }
        if phase != WorkerState::Running {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "only running work can enter graceful drain",
            ));
        }
        self.transition(WorkerState::Draining)
    }
    /// Enter recovery while preserving the active execution reservation.
    pub fn begin_recovery(&self) -> FaultResult<()> {
        self.transition(WorkerState::Recovering)?;
        self.release_reservation();
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        state.ticket = None;
        state.fencing = None;
        Ok(())
    }
    /// Return a recovered worker to the ready state after abandoning any
    /// previously leased attempt. A retry must arrive with a fresh ticket/fence.
    pub fn recovered(&self) -> FaultResult<()> {
        if self
            .reservation
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .is_some()
        {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "worker cannot return to ready while an execution reservation is active",
            ));
        }
        self.transition(WorkerState::Ready)
    }
    pub fn begin_commit(&self, token: FencingToken) -> FaultResult<()> {
        self.require_current_fence(token)?;
        self.transition(WorkerState::Committing)
    }
    pub fn complete(&self, token: FencingToken) -> FaultResult<()> {
        self.require_current_fence(token)?;
        self.transition(WorkerState::Completed)?;
        self.release_reservation();
        Ok(())
    }
    /// Mark active work failed and release its node budget reservation.
    pub fn fail(&self, message: impl Into<String>) -> FaultResult<()> {
        let message = message.into();
        if message.is_empty() || message.len() > 1024 || message.trim() != message {
            return Err(Fault::invalid_argument("worker failure message is invalid"));
        }
        let phase = self.state();
        if state::is_terminal(phase) {
            return Ok(());
        }
        self.transition(WorkerState::Failed)?;
        self.release_reservation();
        Ok(())
    }
    /// Forced cancellation. Unlike graceful drain, this releases the execution
    /// reservation once the state reaches `Cancelled`.
    pub fn cancel(&self, reason: impl Into<String>) -> FaultResult<()> {
        let reason = reason.into();
        if reason.is_empty() || reason.len() > 1024 || reason.trim() != reason {
            return Err(Fault::invalid_argument("worker cancellation reason is invalid"));
        }
        let phase = self.state();
        if state::is_terminal(phase) {
            self.release_reservation();
            return Ok(());
        }
        self.cancellation.cancel(reason);
        if phase != WorkerState::Cancelling {
            self.transition(WorkerState::Cancelling)?;
        }
        self.transition(WorkerState::Cancelled)?;
        self.release_reservation();
        Ok(())
    }
    pub fn next_status_sequence(&self) -> FaultResult<u64> {
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let sequence = state.next_status_sequence;
        state.next_status_sequence = sequence
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "worker status sequence exhausted"))?;
        Ok(sequence)
    }
    #[must_use]
    pub fn diagnostics(&self) -> DiagnosticSnapshot {
        let state = self
            .state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        DiagnosticSnapshot {
            state: state.phase,
            ticket_id: state.ticket.clone(),
            next_status_sequence: state.next_status_sequence,
            cancelled: self.cancellation.is_cancelled(),
            cancellation_reason: self.cancellation.reason(),
        }
    }
    fn require_current_fence(&self, token: FencingToken) -> FaultResult<()> {
        let state = self
            .state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let current = state.fencing.ok_or_else(|| {
            Fault::new(Code::FailedPrecondition, "worker has no fencing token")
        })?;
        commit::require_current(token, current)
    }
    fn transition(&self, to: WorkerState) -> FaultResult<()> {
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if !machine::allowed(state.phase, to) {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "invalid worker state transition",
            )
            .with_context("from", format!("{:?}", state.phase))
            .with_context("to", format!("{to:?}")));
        }
        state.phase = to;
        Ok(())
    }
    fn release_reservation(&self) {
        self.reservation
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .take();
    }
}
