// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Explicit worker state-transition table.
use mindclade_worker_protocol::WorkerState;
#[must_use]
pub const fn allowed(from: WorkerState, to: WorkerState) -> bool {
    use WorkerState::{
        Cancelled, Cancelling, Committing, Completed, Created, Draining, Failed, Leased, Ready,
        Recovering, Running, Starting,
    };
    matches!(
        (from, to),
        (Created, Starting | Cancelling)
            | (Starting | Recovering, Ready)
            | (Ready, Leased | Recovering | Cancelling | Failed)
            | (Leased, Running | Cancelling | Failed)
            | (
                Running,
                Draining | Committing | Recovering | Cancelling | Failed
            )
            | (Draining, Committing | Cancelling | Failed)
            | (Committing, Completed | Cancelling | Failed)
            | (Starting, Recovering | Cancelling | Failed)
            | (Recovering, Cancelling | Failed)
            | (Cancelling, Cancelled)
    )
}
#[must_use]
pub const fn terminal(state: WorkerState) -> bool {
    matches!(
        state,
        WorkerState::Completed | WorkerState::Cancelled | WorkerState::Failed
    )
}
