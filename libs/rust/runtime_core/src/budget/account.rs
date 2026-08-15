// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Named account over a node-wide hierarchical resource budget.
use super::{Budget, BudgetSnapshot, Reservation, ResourceVector};
use mindclade_faults::FaultResult;
use std::sync::Arc;

#[derive(Clone, Debug)]
pub struct ResourceAccount {
    name: Arc<str>,
    budget: Arc<Budget>,
}
impl ResourceAccount {
    #[must_use]
    pub fn root(name: impl Into<Arc<str>>, limits: ResourceVector) -> Self {
        let name = name.into();
        Self {
            budget: Budget::root(name.clone(), limits),
            name,
        }
    }
    #[must_use]
    pub fn child(&self, name: impl Into<Arc<str>>, limits: ResourceVector) -> Self {
        let name = name.into();
        Self {
            budget: Budget::child(self.budget.clone(), name.clone(), limits),
            name,
        }
    }
    #[must_use]
    pub fn name(&self) -> &str {
        &self.name
    }
    #[must_use]
    pub fn budget(&self) -> Arc<Budget> {
        self.budget.clone()
    }
    pub fn reserve(&self, resources: ResourceVector) -> FaultResult<Reservation> {
        self.budget.reserve(resources)
    }
    #[must_use]
    pub fn snapshot(&self) -> BudgetSnapshot {
        BudgetSnapshot::from_budget(&self.budget)
    }
}
