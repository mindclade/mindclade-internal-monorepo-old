// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Process lifecycle, readiness, drain, and supervised task mechanisms.
#![forbid(unsafe_code)]

mod component;
pub mod config;
mod health;
mod lifecycle;
pub mod server;
mod service;
mod shutdown;
pub mod signals;
mod supervisor;
pub use component::{Component, FnComponent};
pub use config::ServiceConfig;
pub use health::{HealthRegistry, HealthReport, HealthStatus};
pub use lifecycle::{Lifecycle, LifecycleState};
pub use service::Service;
pub use shutdown::ShutdownToken;
pub use signals::SignalHandle;
pub use supervisor::{Supervisor, TaskFailure};
