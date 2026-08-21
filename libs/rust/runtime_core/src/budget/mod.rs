// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Hierarchical node-wide resource accounting.
//!
//! Every substantial allocation reserves against the complete ancestor chain.
//! Snapshots expose limits, reservations, and rejection counters as a tree so
//! admission decisions are explainable without relying on OS/GPU free-memory
//! observations alone.

use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeMap;
use std::sync::{Arc, Mutex, Weak};

#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub enum ResourceKind {
    CpuMillis,
    ResidentMemoryBytes,
    PinnedMemoryBytes,
    SharedMemoryBytes,
    LocalDiskBytes,
    OpenFileDescriptors,
    ObjectStoreRequests,
    QueuedRequests,
    Processes,
    CpuThreads,
    GpuMemoryEstimateBytes,
    CheckpointStagingBytes,
    TelemetrySpoolBytes,
    MaximumOutputBytes,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ResourceVector(BTreeMap<ResourceKind, u64>);

impl ResourceVector {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    #[must_use]
    pub fn set(mut self, kind: ResourceKind, amount: u64) -> Self {
        self.0.insert(kind, amount);
        self
    }

    #[must_use]
    pub fn get(&self, kind: ResourceKind) -> u64 {
        self.0.get(&kind).copied().unwrap_or(0)
    }

    pub fn iter(&self) -> impl Iterator<Item = (ResourceKind, u64)> + '_ {
        self.0.iter().map(|(kind, value)| (*kind, *value))
    }
}

#[derive(Debug)]
struct State {
    limits: ResourceVector,
    used: ResourceVector,
    corrupted: bool,
    rejections: u64,
}

#[derive(Clone, Debug)]
pub struct Budget {
    name: Arc<str>,
    state: Arc<Mutex<State>>,
    parent: Option<Arc<Budget>>,
    children: Arc<Mutex<Vec<Weak<Budget>>>>,
}

impl Budget {
    #[must_use]
    pub fn root(name: impl Into<Arc<str>>, limits: ResourceVector) -> Arc<Self> {
        Arc::new(Self {
            name: name.into(),
            state: Arc::new(Mutex::new(State {
                limits,
                used: ResourceVector::new(),
                corrupted: false,
                rejections: 0,
            })),
            parent: None,
            children: Arc::new(Mutex::new(Vec::new())),
        })
    }

    #[must_use]
    pub fn child(
        parent: Arc<Self>,
        name: impl Into<Arc<str>>,
        limits: ResourceVector,
    ) -> Arc<Self> {
        let parent_children = parent.children.clone();
        let child = Arc::new(Self {
            name: name.into(),
            state: Arc::new(Mutex::new(State {
                limits,
                used: ResourceVector::new(),
                corrupted: false,
                rejections: 0,
            })),
            parent: Some(parent),
            children: Arc::new(Mutex::new(Vec::new())),
        });
        parent_children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .push(Arc::downgrade(&child));
        child
    }

    #[must_use]
    pub fn name(&self) -> &str {
        &self.name
    }

    pub fn reserve(self: &Arc<Self>, requested: ResourceVector) -> FaultResult<Reservation> {
        let parent_reservation = match &self.parent {
            Some(parent) => Some(Box::new(parent.reserve(requested.clone())?)),
            None => None,
        };

        {
            let mut state = self
                .state
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner);
            if state.corrupted {
                return Err(Fault::data_loss("resource budget accounting is corrupted"));
            }

            for (kind, amount) in requested.iter() {
                let next =
                    state.used.get(kind).checked_add(amount).ok_or_else(|| {
                        Fault::new(Code::OutOfRange, "resource accounting overflow")
                    })?;
                let limit = state.limits.get(kind);
                if limit != 0 && next > limit {
                    state.rejections = state.rejections.checked_add(1).ok_or_else(|| {
                        Fault::new(Code::OutOfRange, "resource rejection counter overflow")
                    })?;
                    return Err(
                        Fault::new(Code::ResourceExhausted, "resource budget exceeded")
                            .with_context("budget", self.name.to_string())
                            .with_context("resource", format!("{kind:?}"))
                            .with_context("requested", amount)
                            .with_context("limit", limit),
                    );
                }
            }

            for (kind, amount) in requested.iter() {
                let next =
                    state.used.get(kind).checked_add(amount).ok_or_else(|| {
                        Fault::new(Code::OutOfRange, "resource accounting overflow")
                    })?;
                state.used.0.insert(kind, next);
            }
        }

        Ok(Reservation::new(
            Arc::downgrade(self),
            requested,
            parent_reservation,
        ))
    }

    #[must_use]
    pub fn is_corrupted(&self) -> bool {
        self.state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .corrupted
    }

    #[must_use]
    pub fn snapshot(&self) -> (ResourceVector, ResourceVector, u64) {
        let state = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        (state.limits.clone(), state.used.clone(), state.rejections)
    }

    #[must_use]
    pub fn tree_snapshot(self: &Arc<Self>) -> BudgetTreeSnapshot {
        BudgetTreeSnapshot::from_budget(self)
    }

    fn live_children(&self) -> Vec<Arc<Budget>> {
        let mut children = self
            .children
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let mut live = Vec::new();
        children.retain(|child| {
            if let Some(child) = child.upgrade() {
                live.push(child);
                true
            } else {
                false
            }
        });
        live
    }
}

pub mod account;
pub mod allocation;
pub mod hierarchy;
pub mod limits;
mod reservation;
pub mod snapshot;
pub mod tracker;

pub use account::ResourceAccount;
pub use allocation::{Allocation, AllocationRequest};
pub use hierarchy::BudgetHierarchy;
pub use limits::ResourceLimits;
pub use reservation::Reservation;
pub use snapshot::{BudgetSnapshot, BudgetTreeSnapshot};
pub use tracker::ResourceTracker;
