// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! `servicekit` adapter for the runtime-host core.

use crate::{HostCore, HostHealth};
use mindclade_faults::FaultResult;
use mindclade_servicekit::Component;
use std::sync::Arc;

/// Binds the host core to the deterministic process lifecycle.
pub struct HostComponent {
    core: Arc<HostCore>,
    health: Arc<HostHealth>,
}

impl core::fmt::Debug for HostComponent {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("HostComponent")
            .field("health", &self.health.snapshot())
            .finish_non_exhaustive()
    }
}

impl HostComponent {
    #[must_use]
    pub fn new(core: Arc<HostCore>, health: Arc<HostHealth>) -> Self {
        Self { core, health }
    }
}

impl Component for HostComponent {
    fn name(&self) -> &str {
        "runtime-host-core"
    }
    /// Any preloaded model is already resident by the time this runs; the only
    /// thing start adds is ordering, so admission opens under the lifecycle
    /// rather than beside it.
    fn start(&mut self) -> FaultResult<()> {
        self.health.set_accepting(true);
        Ok(())
    }
    /// Stop admitting new sessions without terminating resident work.
    fn drain(&mut self) -> FaultResult<()> {
        self.core.begin_drain();
        Ok(())
    }
    /// Release worker processes and model slots.
    ///
    /// Idempotent with `drain` on purpose: `Service` calls `stop` during startup
    /// rollback, where `drain` has never run.
    fn stop(&mut self) -> FaultResult<()> {
        self.core.begin_drain();
        self.core.shutdown()
    }
}
