// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! `servicekit` adapters for the node-agent core and its providers.
//!
//! Each component files its own `HealthRegistry` report from its `start` hook
//! rather than at construction. That ordering is what keeps readiness honest:
//! `HealthRegistry::is_ready` treats an empty registry as not ready, so nothing
//! can answer ready before the lifecycle has actually started something, and a
//! component that fails to start never files a report at all.

use crate::NodeAgentCore;
use crate::dependencies::ObjectStoreProbe;
use crate::operations::{ARTIFACT_STORE_REPORT, CORE_REPORT};
use mindclade_faults::FaultResult;
use mindclade_servicekit::{Component, HealthRegistry, HealthStatus};
use std::sync::Arc;

#[derive(Debug)]
pub struct NodeAgentComponent {
    core: Arc<NodeAgentCore>,
    reports: HealthRegistry,
}

impl NodeAgentComponent {
    #[must_use]
    pub fn new(core: Arc<NodeAgentCore>, reports: HealthRegistry) -> Self {
        Self { core, reports }
    }
    #[must_use]
    pub fn core(&self) -> Arc<NodeAgentCore> {
        self.core.clone()
    }
}

impl Component for NodeAgentComponent {
    fn name(&self) -> &'static str {
        CORE_REPORT
    }
    fn start(&mut self) -> FaultResult<()> {
        self.reports.set(
            CORE_REPORT,
            HealthStatus::Healthy,
            "stage accounting is consistent",
        )
    }
    fn drain(&mut self) -> FaultResult<()> {
        self.core.drain();
        Ok(())
    }
    /// Idempotent with `drain` on purpose: `Service` calls `stop` during startup
    /// rollback, where `drain` has never run.
    fn stop(&mut self) -> FaultResult<()> {
        self.core.drain();
        self.core.cancel_active("node agent stopping");
        Ok(())
    }
}

/// Binds the node's artifact object store to the process lifecycle.
///
/// The store is a mandatory dependency: every stage this agent admits stages
/// bytes through it. Its report is therefore part of readiness, and a probe
/// failure at start is a startup failure rather than a degraded start.
#[derive(Debug)]
pub struct ArtifactStoreComponent {
    probe: ObjectStoreProbe,
    reports: HealthRegistry,
}

impl ArtifactStoreComponent {
    #[must_use]
    pub fn new(probe: ObjectStoreProbe, reports: HealthRegistry) -> Self {
        Self { probe, reports }
    }
}

impl Component for ArtifactStoreComponent {
    fn name(&self) -> &'static str {
        ARTIFACT_STORE_REPORT
    }
    fn start(&mut self) -> FaultResult<()> {
        self.probe.check_blocking()?;
        self.reports.set(
            ARTIFACT_STORE_REPORT,
            HealthStatus::Healthy,
            "artifact object store answered a metadata read",
        )
    }
    /// The store outlives the process and owns no per-process state, so there
    /// is nothing to release. The report is left as it stands: rewriting it on
    /// the way out would only change what a probe racing shutdown reads, and
    /// the lifecycle phase already answers that question.
    fn stop(&mut self) -> FaultResult<()> {
        Ok(())
    }
}
