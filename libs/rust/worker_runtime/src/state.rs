// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

pub use mindclade_worker_protocol::WorkerState;
#[must_use]
pub const fn is_terminal(state: WorkerState) -> bool {
    matches!(
        state,
        WorkerState::Completed | WorkerState::Cancelled | WorkerState::Failed
    )
}
