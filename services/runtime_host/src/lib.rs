// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Node-local runtime host around process-isolated Python/PyTorch workers.
//!
//! The host owns process/resource envelopes, ticket revalidation, model slots,
//! cancellation, drain, and IPC bounds. Python remains authoritative for tensor
//! batching and model numerics.
#![forbid(unsafe_code)]

pub mod async_ipc;
pub mod authority;
pub mod bootstrap;
pub mod bulk;
pub mod config;
pub mod grpc;
pub mod health;
pub mod ipc;
pub mod lifecycle;
pub mod models;
pub mod processes;
pub mod protocol;
pub mod resources;
pub mod server;
pub mod supervision;
pub mod telemetry;
pub mod worker_ipc;
pub use authority::HostAuthority;
pub use bulk::{BulkBackend, BulkBufferBroker};
pub use config::HostConfig;
pub use health::{HostHealth, HostHealthSnapshot};
pub use lifecycle::HostComponent;
pub use models::{LoadedModel, ModelRegistry, ModelSpec};
pub use processes::{
    ProcessHandle, ProcessLauncher, ProcessSpec, ProcessSupervisor, StdProcessLauncher,
};
pub use resources::NodeResources;
pub use server::{ExecutionSession, HostCore};
