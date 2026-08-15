// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Standard node/service/worker/request budget hierarchy.
use super::{Budget, ResourceVector};
use std::sync::Arc;
#[derive(Clone, Debug)]
pub struct BudgetHierarchy {
    root: Arc<Budget>,
}
impl BudgetHierarchy {
    #[must_use]
    pub fn new(node_limits: ResourceVector) -> Self {
        Self {
            root: Budget::root("node", node_limits),
        }
    }
    #[must_use]
    pub fn root(&self) -> Arc<Budget> {
        self.root.clone()
    }
    #[must_use]
    pub fn service(&self, name: impl Into<Arc<str>>, limits: ResourceVector) -> Arc<Budget> {
        Budget::child(self.root.clone(), name, limits)
    }
    #[must_use]
    pub fn child(
        parent: Arc<Budget>,
        name: impl Into<Arc<str>>,
        limits: ResourceVector,
    ) -> Arc<Budget> {
        Budget::child(parent, name, limits)
    }
}
