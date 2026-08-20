// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Execution-only primitives shared by Rust runtime/data-plane components.
#![forbid(unsafe_code)]
mod budget;
mod byte_semaphore;
mod cancellation;
mod clock;
mod deadline;
mod fencing;
mod resource_version;
mod retry;
mod task_group;
// BudgetTreeSnapshot was missing here while `Budget::tree_snapshot()` is public and returns
// it — a public method whose return type callers could not name. node_agent is the caller
// that surfaced it.
pub use budget::{
    Allocation, AllocationRequest, Budget, BudgetHierarchy, BudgetSnapshot, BudgetTreeSnapshot,
    Reservation, ResourceAccount, ResourceKind, ResourceLimits, ResourceTracker, ResourceVector,
};
pub use byte_semaphore::{BytePermit, ByteSemaphore, ByteSemaphoreSnapshot};
pub use cancellation::CancellationToken;
pub use clock::{Clock, ManualClock, SystemClock};
pub use deadline::Deadline;
pub use fencing::FencingToken;
pub use resource_version::{Precondition, ResourceVersion};
pub use retry::{Policy, Sleeper, ThreadSleeper, execute};
pub use task_group::{TaskFailure, TaskGroup, TaskReport};
