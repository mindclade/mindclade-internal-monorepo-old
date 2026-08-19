// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! `servicekit` adapter for the runtime-gateway core.

use crate::{GatewayCore, GatewayHealth};
use mindclade_faults::FaultResult;
use mindclade_servicekit::Component;
use std::sync::Arc;

/// Binds the gateway core to the deterministic process lifecycle.
pub struct GatewayComponent {
    core: Arc<GatewayCore>,
    health: Arc<GatewayHealth>,
}

impl core::fmt::Debug for GatewayComponent {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("GatewayComponent")
            .field("health", &self.health.snapshot())
            .finish_non_exhaustive()
    }
}

impl GatewayComponent {
    #[must_use]
    pub fn new(core: Arc<GatewayCore>, health: Arc<GatewayHealth>) -> Self {
        Self { core, health }
    }
}

impl Component for GatewayComponent {
    fn name(&self) -> &str {
        "runtime-gateway-core"
    }
    /// Readiness is asserted HERE rather than at construction.
    ///
    /// The policy artifacts are already verified by the time this runs, so the
    /// only thing this adds is ordering: nothing advertises itself as accepting
    /// until the lifecycle has actually entered startup.
    fn start(&mut self) -> FaultResult<()> {
        self.health.set_policy_fresh(true);
        self.health.set_accepting(true);
        Ok(())
    }
    /// Stop admitting new work without severing what is already established.
    fn drain(&mut self) -> FaultResult<()> {
        self.core.begin_drain();
        Ok(())
    }
    /// Idempotent with `drain` on purpose: `Service` calls `stop` during startup
    /// rollback, where `drain` has never run.
    fn stop(&mut self) -> FaultResult<()> {
        self.core.begin_drain();
        Ok(())
    }
}
