// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Node-wide hierarchical resource-budget root.

use mindclade_faults::FaultResult;
use mindclade_runtime_core::{Budget, ResourceVector};
use std::sync::Arc;

#[derive(Clone, Debug)]
pub struct NodeResources {
    root: Arc<Budget>,
}
impl NodeResources {
    pub fn new(limits: ResourceVector) -> FaultResult<Self> {
        Ok(Self {
            root: Budget::root("runtime-host-node", limits),
        })
    }
    #[must_use]
    pub fn root(&self) -> Arc<Budget> {
        self.root.clone()
    }
    #[must_use]
    pub fn child(&self, name: impl Into<Arc<str>>, limits: ResourceVector) -> Arc<Budget> {
        Budget::child(self.root.clone(), name, limits)
    }
}
