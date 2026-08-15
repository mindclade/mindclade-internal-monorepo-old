// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_worker_protocol::WorkerState;
use mindclade_worker_runtime::machine::allowed;
#[test]
fn terminal_transition_graph_is_explicit() {
    assert!(allowed(WorkerState::Created, WorkerState::Starting));
    assert!(!allowed(WorkerState::Completed, WorkerState::Running));
}
