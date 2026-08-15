// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic service lifecycle with explicit drain semantics.

use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::{Arc, Mutex};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LifecycleState {
    Created,
    Starting,
    Running,
    Draining,
    Stopping,
    Stopped,
    Failed,
}

#[derive(Clone, Debug)]
pub struct Lifecycle {
    state: Arc<Mutex<LifecycleState>>,
}

impl Default for Lifecycle {
    fn default() -> Self {
        Self::new()
    }
}

impl Lifecycle {
    #[must_use]
    pub fn new() -> Self {
        Self {
            state: Arc::new(Mutex::new(LifecycleState::Created)),
        }
    }
    #[must_use]
    pub fn state(&self) -> LifecycleState {
        *self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
    }
    pub fn transition(&self, to: LifecycleState) -> FaultResult<()> {
        let mut s = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if !allowed(*s, to) {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "invalid service lifecycle transition",
            )
            .with_context("from", format!("{:?}", *s))
            .with_context("to", format!("{to:?}")));
        }
        *s = to;
        Ok(())
    }
}

fn allowed(from: LifecycleState, to: LifecycleState) -> bool {
    use LifecycleState::{Created, Draining, Failed, Running, Starting, Stopped, Stopping};
    matches!(
        (from, to),
        (Created, Starting | Failed)
            | (Starting, Running | Failed)
            | (Running, Draining | Stopping | Failed)
            | (Draining, Stopping | Failed)
            | (Stopping, Stopped | Failed)
    )
}
