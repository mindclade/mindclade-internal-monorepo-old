//! Explicit worker state-transition table.
use mindclade_worker_protocol::WorkerState;
#[must_use]
pub const fn allowed(from: WorkerState, to: WorkerState) -> bool {
    use WorkerState::*;
    matches!((from, to),
        (Created, Starting) | (Starting, Ready) | (Ready, Leased) | (Leased, Running) |
        (Running, Draining) | (Running, Committing) | (Draining, Committing) | (Committing, Completed) |
        (Starting, Recovering) | (Ready, Recovering) | (Running, Recovering) | (Recovering, Ready) |
        (Created, Cancelling) | (Starting, Cancelling) | (Ready, Cancelling) | (Leased, Cancelling) |
        (Running, Cancelling) | (Draining, Cancelling) | (Recovering, Cancelling) | (Committing, Cancelling) |
        (Cancelling, Cancelled) | (Starting, Failed) | (Ready, Failed) | (Leased, Failed) |
        (Running, Failed) | (Draining, Failed) | (Recovering, Failed) | (Committing, Failed))
}
#[must_use] pub const fn terminal(state: WorkerState) -> bool { matches!(state, WorkerState::Completed | WorkerState::Cancelled | WorkerState::Failed) }
