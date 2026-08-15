// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Components registered with the deterministic service lifecycle.

use mindclade_faults::FaultResult;

/// A process-owned component with explicit startup, drain, and stop hooks.
///
/// Implementations must make `drain` and `stop` idempotent. `Service` may call
/// `stop` during startup rollback after a later component fails to start.
pub trait Component: Send {
    /// Stable component name used in diagnostics and failure context.
    fn name(&self) -> &str;
    /// Acquire resources and start background work.
    fn start(&mut self) -> FaultResult<()>;
    /// Reject new work while allowing already-admitted work to finish.
    fn drain(&mut self) -> FaultResult<()> {
        Ok(())
    }
    /// Release resources and terminate background work.
    fn stop(&mut self) -> FaultResult<()>;
}

/// Adapter for small components whose lifecycle is represented by closures.
pub struct FnComponent<S, D, T>
where
    S: FnMut() -> FaultResult<()> + Send,
    D: FnMut() -> FaultResult<()> + Send,
    T: FnMut() -> FaultResult<()> + Send,
{
    name: String,
    start: S,
    drain: D,
    stop: T,
}

impl<S, D, T> FnComponent<S, D, T>
where
    S: FnMut() -> FaultResult<()> + Send,
    D: FnMut() -> FaultResult<()> + Send,
    T: FnMut() -> FaultResult<()> + Send,
{
    #[must_use]
    pub fn new(name: impl Into<String>, start: S, drain: D, stop: T) -> Self {
        Self {
            name: name.into(),
            start,
            drain,
            stop,
        }
    }
}

impl<S, D, T> Component for FnComponent<S, D, T>
where
    S: FnMut() -> FaultResult<()> + Send,
    D: FnMut() -> FaultResult<()> + Send,
    T: FnMut() -> FaultResult<()> + Send,
{
    fn name(&self) -> &str {
        &self.name
    }
    fn start(&mut self) -> FaultResult<()> {
        (self.start)()
    }
    fn drain(&mut self) -> FaultResult<()> {
        (self.drain)()
    }
    fn stop(&mut self) -> FaultResult<()> {
        (self.stop)()
    }
}
