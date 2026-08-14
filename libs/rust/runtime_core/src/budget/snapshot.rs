//! Immutable resource-accounting snapshots.

use super::{Budget, ResourceKind, ResourceVector};
use std::sync::Arc;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetSnapshot {
    pub name: String,
    pub limits: ResourceVector,
    pub reserved: ResourceVector,
    pub used_estimate: ResourceVector,
    pub waiters: u64,
    pub rejections: u64,
    pub corrupted: bool,
}

impl BudgetSnapshot {
    #[must_use]
    pub fn from_budget(budget: &Budget) -> Self {
        let (limits, reserved, rejections) = budget.snapshot();
        Self {
            name: budget.name().to_owned(),
            limits,
            used_estimate: reserved.clone(),
            reserved,
            waiters: 0,
            rejections,
            corrupted: budget.is_corrupted(),
        }
    }

    #[must_use]
    pub fn remaining(&self, kind: ResourceKind) -> Option<u64> {
        let limit = self.limits.get(kind);
        if limit == 0 {
            return None;
        }
        limit.checked_sub(self.reserved.get(kind))
    }

    #[must_use]
    pub fn utilization_permyriad(&self, kind: ResourceKind) -> Option<u16> {
        let limit = self.limits.get(kind);
        if limit == 0 {
            return None;
        }
        let value = (u128::from(self.reserved.get(kind).min(limit)) * 10_000)
            / u128::from(limit);
        u16::try_from(value).ok()
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetTreeSnapshot {
    pub budget: BudgetSnapshot,
    pub children: Vec<BudgetTreeSnapshot>,
}

impl BudgetTreeSnapshot {
    #[must_use]
    pub fn from_budget(budget: &Arc<Budget>) -> Self {
        let mut children: Vec<_> = budget
            .live_children()
            .iter()
            .map(Self::from_budget)
            .collect();
        children.sort_by(|left, right| left.budget.name.cmp(&right.budget.name));
        Self {
            budget: BudgetSnapshot::from_budget(budget),
            children,
        }
    }

    #[must_use]
    pub fn find(&self, name: &str) -> Option<&Self> {
        if self.budget.name == name {
            return Some(self);
        }
        self.children.iter().find_map(|child| child.find(name))
    }
}
