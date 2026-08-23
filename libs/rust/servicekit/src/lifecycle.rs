// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic service lifecycle with explicit drain semantics.
//!
//! The phase graph here is the same one the Go control plane runs
//! (`libs/go/servicekit/state.go`, `State.CanTransitionTo`). It has to be: a
//! node's phase only means something to a fleet controller if both runtimes
//! name the same seven phases and admit the same transitions between them.

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

impl LifecycleState {
    /// The stable phase name shared with the Go runtime.
    ///
    /// These are the strings `libs/go/servicekit` reports as `service_state`,
    /// so a phase logged by a Rust node and a phase logged by a Go process are
    /// the same token rather than two spellings an operator has to reconcile.
    /// `Created` reports `new` for that reason — it is Go's `StateNew`, and the
    /// Rust variant keeps the name that reads correctly in Rust.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Created => "new",
            Self::Starting => "starting",
            Self::Running => "running",
            Self::Draining => "draining",
            Self::Stopping => "stopping",
            Self::Stopped => "stopped",
            Self::Failed => "failed",
        }
    }
    /// Reports whether no further transition is expected.
    #[must_use]
    pub const fn is_terminal(self) -> bool {
        matches!(self, Self::Stopped | Self::Failed)
    }
    /// Reports whether the phase admits new work.
    ///
    /// Exactly one phase does. A draining process still answers established
    /// requests, but advertising readiness there is what makes an orchestrator
    /// keep routing new traffic into a process that is on its way out.
    #[must_use]
    pub const fn admits_traffic(self) -> bool {
        matches!(self, Self::Running)
    }
    /// Reports whether the process is alive from an orchestrator's point of
    /// view: it has begun and has not reached a terminal phase.
    ///
    /// Shutdown phases are deliberately live. A liveness probe that fails
    /// during drain gets the process killed mid-request, which is the opposite
    /// of what the drain exists to do.
    #[must_use]
    pub const fn is_live(self) -> bool {
        matches!(
            self,
            Self::Starting | Self::Running | Self::Draining | Self::Stopping
        )
    }
    /// Reports whether `to` is a legal successor of this phase.
    ///
    /// Two edges out of `Starting` exist because a process must be able to end
    /// without ever having served: `Stopping` is startup failure, `Draining` is
    /// a termination request that arrives before `Running` is announced.
    /// Neither passes through `Running`, which is what keeps readiness false
    /// for a process that never admitted traffic.
    #[must_use]
    pub const fn can_transition_to(self, to: Self) -> bool {
        matches!(
            (self, to),
            (Self::Created, Self::Starting | Self::Failed)
                | (
                    Self::Starting,
                    Self::Running | Self::Draining | Self::Stopping | Self::Failed
                )
                | (
                    Self::Running,
                    Self::Draining | Self::Stopping | Self::Failed
                )
                | (Self::Draining, Self::Stopping | Self::Failed)
                | (Self::Stopping, Self::Stopped | Self::Failed)
        )
    }
}

impl core::fmt::Display for LifecycleState {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter.write_str(self.as_str())
    }
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
        if !s.can_transition_to(to) {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "invalid service lifecycle transition",
            )
            // Field names match the Go runtime's lifecycle event attributes so
            // one query finds a bad transition in either language.
            .with_context("state_from", s.as_str())
            .with_context("state_to", to.as_str()));
        }
        *s = to;
        Ok(())
    }
}
