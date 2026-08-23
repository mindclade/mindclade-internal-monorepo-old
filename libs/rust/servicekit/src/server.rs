// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Network/server lifecycle adapter contracts.

use crate::{HealthRegistry, Service};
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
///
/// Both halves are required, and this is the only pair that answers a readiness
/// probe correctly. `HealthRegistry::is_ready` on its own reports dependency
/// health: it says nothing about the phase, so a draining process whose
/// dependencies are all healthy still reports ready and keeps receiving new
/// traffic until its listener dies. The Go runtime answers readiness from the
/// phase for exactly this reason (`libs/go/servicekit`: ready in `running`
/// only), and a Rust node has to agree or the two are routed differently.
#[must_use]
pub fn ready(service: &Service, health: &HealthRegistry) -> bool {
    service.lifecycle().state().admits_traffic() && health.is_ready()
}

/// Liveness is a conjunction of service lifecycle and dependency health.
///
/// Shutdown phases stay live so a clean drain is not killed halfway, but a
/// process that has failed or has finished stopping is not: reporting live
/// there leaves a dead process in service until something else notices.
/// `HealthRegistry::is_live` alone cannot see either condition, because a
/// failed lifecycle leaves no unhealthy dependency report behind.
#[must_use]
pub fn live(service: &Service, health: &HealthRegistry) -> bool {
    service.lifecycle().state().is_live() && health.is_live()
}
