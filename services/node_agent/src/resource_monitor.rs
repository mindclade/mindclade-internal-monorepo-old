// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Node-wide resource-accounting views used by admission, telemetry and diagnostics.
use mindclade_runtime_core::{Budget, BudgetSnapshot, BudgetTreeSnapshot, ResourceTracker};
use std::sync::Arc;

#[derive(Clone, Debug)]
pub struct ResourceMonitor {
    root: Arc<Budget>,
    tracker: ResourceTracker,
}
impl ResourceMonitor {
    #[must_use]
    pub fn new(root: Arc<Budget>) -> Self {
        Self {
            tracker: ResourceTracker::new(root.clone()),
            root,
        }
    }
    #[must_use]
    pub fn snapshot(&self) -> BudgetSnapshot {
        self.tracker.snapshot()
    }
    #[must_use]
    pub fn tree(&self) -> BudgetTreeSnapshot {
        self.root.tree_snapshot()
    }
}
