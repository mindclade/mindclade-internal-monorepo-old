//! Execution-only primitives shared by Rust runtime/data-plane components.
#![forbid(unsafe_code)]
mod budget;
mod cancellation;
mod clock;
mod deadline;
mod fencing;
mod resource_version;
mod retry;
mod task_group;
pub use budget::{
    Allocation, AllocationRequest, Budget, BudgetHierarchy, BudgetSnapshot, Reservation, ResourceAccount,
    ResourceKind, ResourceLimits, ResourceTracker, ResourceVector
};
pub use cancellation::CancellationToken;
pub use clock::{
    Clock, ManualClock, SystemClock
};
pub use deadline::Deadline;
pub use fencing::FencingToken;
pub use resource_version::{
    Precondition, ResourceVersion
};
pub use retry::{
    execute, Policy, Sleeper, ThreadSleeper
};
pub use task_group::{
    TaskFailure, TaskGroup, TaskReport
};
