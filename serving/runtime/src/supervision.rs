// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Model-process lifecycle state independent of operating-system launcher.

use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::{Arc, Mutex};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProcessState {
    Created,
    Starting,
    Ready,
    Draining,
    Stopped,
    Failed,
}

#[derive(Clone, Debug)]
pub struct ProcessLifecycle {
    state: Arc<Mutex<ProcessState>>,
}

impl ProcessLifecycle {
    #[must_use]
    pub fn new() -> Self {
        Self {
            state: Arc::new(Mutex::new(ProcessState::Created)),
        }
    }
    #[must_use]
    pub fn state(&self) -> ProcessState {
        *self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
    }
    pub fn transition(&self, to: ProcessState) -> FaultResult<()> {
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let allowed = matches!(
            (*state, to),
            (ProcessState::Created, ProcessState::Starting)
                | (
                    ProcessState::Starting,
                    ProcessState::Ready | ProcessState::Failed
                )
                | (
                    ProcessState::Ready,
                    ProcessState::Draining | ProcessState::Failed
                )
                | (
                    ProcessState::Draining,
                    ProcessState::Stopped | ProcessState::Failed
                )
        );
        if !allowed {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "invalid model process transition",
            ));
        }
        *state = to;
        Ok(())
    }
}

impl Default for ProcessLifecycle {
    fn default() -> Self {
        Self::new()
    }
}
