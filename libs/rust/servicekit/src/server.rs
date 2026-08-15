// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Network/server lifecycle adapter contracts.

use crate::{HealthRegistry, LifecycleState, Service};
use mindclade_faults::FaultResult;

/// Narrow adapter for network servers owned by a service composition root.
pub trait ServerAdapter: Send {
    /// Begin accepting new work after all mandatory dependencies are ready.
    fn start_accepting(&mut self) -> FaultResult<()>;
    /// Stop admission without terminating established work.
    fn stop_accepting(&mut self) -> FaultResult<()>;
    /// Terminate established work after the drain budget expires or completes.
    fn shutdown(&mut self) -> FaultResult<()>;
}

/// Readiness is a conjunction of service lifecycle and dependency health.
#[must_use]
pub fn ready(service: &Service, health: &HealthRegistry) -> bool {
    service.lifecycle().state() == LifecycleState::Running && health.is_ready()
}
