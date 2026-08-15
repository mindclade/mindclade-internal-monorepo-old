// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! `servicekit` adapter for the node-agent core.
use crate::NodeAgentCore;
use mindclade_faults::FaultResult;
use mindclade_servicekit::Component;
use std::sync::Arc;

pub struct NodeAgentComponent {
    core: Arc<NodeAgentCore>,
}
impl NodeAgentComponent {
    #[must_use]
    pub fn new(core: Arc<NodeAgentCore>) -> Self {
        Self { core }
    }
    #[must_use]
    pub fn core(&self) -> Arc<NodeAgentCore> {
        self.core.clone()
    }
}
impl Component for NodeAgentComponent {
    fn name(&self) -> &'static str {
        "node-agent-core"
    }
    fn start(&mut self) -> FaultResult<()> {
        Ok(())
    }
    fn drain(&mut self) -> FaultResult<()> {
        self.core.drain();
        Ok(())
    }
    fn stop(&mut self) -> FaultResult<()> {
        self.core.drain();
        self.core.cancel_active("node agent stopping");
        Ok(())
    }
}
