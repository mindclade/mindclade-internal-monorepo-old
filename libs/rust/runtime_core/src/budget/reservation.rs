// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! RAII ownership for hierarchical resource reservations.

use super::{Budget, ResourceVector};
use std::sync::Weak;

#[derive(Debug)]
pub struct Reservation {
    budget: Weak<Budget>,
    resources: ResourceVector,
    parent: Option<Box<Reservation>>,
}

impl Reservation {
    pub(super) fn new(
        budget: Weak<Budget>,
        resources: ResourceVector,
        parent: Option<Box<Self>>,
    ) -> Self {
        Self {
            budget,
            resources,
            parent,
        }
    }
}

impl Drop for Reservation {
    fn drop(&mut self) {
        if let Some(budget) = self.budget.upgrade() {
            let mut state = budget
                .state
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner);
            for (kind, amount) in self.resources.iter() {
                if let Some(next) = state.used.get(kind).checked_sub(amount) {
                    state.used.0.insert(kind, next);
                } else {
                    state.corrupted = true;
                    state.used.0.insert(kind, 0);
                }
            }
        }
        let _ = self.parent.take();
    }
}
