// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! `servicekit` adapter for the artifact-proxy core.
use crate::ArtifactProxyCore;
use mindclade_faults::FaultResult;
use mindclade_servicekit::Component;
use std::sync::Arc;

pub struct ArtifactProxyComponent {
    core: Arc<ArtifactProxyCore>,
}
impl ArtifactProxyComponent {
    #[must_use]
    pub fn new(core: Arc<ArtifactProxyCore>) -> Self { Self { core } }
    #[must_use]
    pub fn core(&self) -> Arc<ArtifactProxyCore> { self.core.clone() }
}
impl Component for ArtifactProxyComponent {
    fn name(&self) -> &str { "artifact-proxy-core" }
    fn start(&mut self) -> FaultResult<()> { Ok(()) }
    fn drain(&mut self) -> FaultResult<()> { self.core.drain(); Ok(()) }
    fn stop(&mut self) -> FaultResult<()> { self.core.drain(); Ok(()) }
}
