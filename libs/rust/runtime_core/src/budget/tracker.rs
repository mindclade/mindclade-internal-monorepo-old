// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Lightweight tracker used by health/telemetry adapters.
use super::{Budget, BudgetSnapshot};
use std::sync::Arc;

#[derive(Clone, Debug)]
pub struct ResourceTracker {
    budget: Arc<Budget>,
}

impl ResourceTracker {
    #[must_use]
    pub fn new(budget: Arc<Budget>) -> Self {
        Self { budget }
    }
    #[must_use]
    pub fn snapshot(&self) -> BudgetSnapshot {
        BudgetSnapshot::from_budget(&self.budget)
    }
}
