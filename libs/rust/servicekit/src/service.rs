// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Ordered component assembly with transactional startup and exhaustive shutdown.

use crate::config::{DEFAULT_DRAIN_TIMEOUT, DEFAULT_SHUTDOWN_TIMEOUT};
use crate::{Component, Lifecycle, LifecycleState, ServiceConfig};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::{Clock, SystemClock};
use std::sync::Arc;
use std::time::{Duration, Instant};

/// Deterministic process lifecycle owner.
///
/// Components start in registration order and drain/stop in reverse order.
/// Startup is transactional: if component `N` fails to start, all previously
/// started components are stopped before the startup error is returned.
///
/// Drain and stop are bounded. Both passes carry a deadline and stop calling
/// hooks once it expires, because an unbounded shutdown is not a slow shutdown:
/// the orchestrator's grace period ends and the process is killed with SIGKILL
/// mid-request, which is exactly what the drain exists to avoid. The bound is a
/// budget for the pass rather than a preemption of one hook — hooks are
/// synchronous `&mut self` calls and cannot be cancelled from here, so a hook
/// that blocks forever still blocks the process. Hooks must return.
pub struct Service {
    config: ServiceConfig,
    clock: Arc<dyn Clock>,
    lifecycle: Lifecycle,
    components: Vec<Box<dyn Component>>,
    started: usize,
}

impl core::fmt::Debug for Service {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("Service")
            .field("name", &self.config.name)
            .field("state", &self.lifecycle.state())
            .field("components", &self.components.len())
            .field("started", &self.started)
            .field("drain_timeout", &self.config.drain_timeout)
            .field("shutdown_timeout", &self.config.shutdown_timeout)
            .finish_non_exhaustive()
    }
}

impl Service {
    /// Service with the repository-default lifecycle budgets and the system
    /// clock. Use [`Service::with_config`] to name the process or to size its
    /// budgets against a deployment's termination grace period.
    #[must_use]
    pub fn new() -> Self {
        Self {
            // Constructed directly rather than through `ServiceConfig::new`:
            // the defaults are known-valid, and `new` must not fail.
            config: ServiceConfig {
                name: String::from("service"),
                drain_timeout: DEFAULT_DRAIN_TIMEOUT,
                shutdown_timeout: DEFAULT_SHUTDOWN_TIMEOUT,
            },
            clock: Arc::new(SystemClock),
            lifecycle: Lifecycle::new(),
            components: Vec::new(),
            started: 0,
        }
    }
    /// Service with validated configuration and an injected clock.
    pub fn with_config(config: ServiceConfig, clock: Arc<dyn Clock>) -> FaultResult<Self> {
        Ok(Self {
            config: config.validate()?,
            clock,
            lifecycle: Lifecycle::new(),
            components: Vec::new(),
            started: 0,
        })
    }
    /// Register a uniquely named component before startup.
    pub fn register(&mut self, component: Box<dyn Component>) -> FaultResult<()> {
        if self.lifecycle.state() != LifecycleState::Created {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "components cannot be registered after service startup begins",
            ));
        }
        let name = component.name();
        if name.is_empty()
            || name.len() > 128
            || self.components.iter().any(|item| item.name() == name)
        {
            return Err(Fault::invalid_argument(
                "component name is empty, too long, or duplicated",
            ));
        }
        self.components.push(component);
        Ok(())
    }
    /// Start all components transactionally.
    pub fn start(&mut self) -> FaultResult<()> {
        self.lifecycle.transition(LifecycleState::Starting)?;
        for index in 0..self.components.len() {
            if let Err(start_fault) = self.components[index].start() {
                let component_name = self.components[index].name().to_owned();
                // Rollback goes through Stopping rather than jumping to Failed,
                // because rollback *is* a stop pass and a startup failure has to
                // report the same phase sequence in both runtimes: the Go
                // coordinator emits starting -> stopping -> failed here, and a
                // controller validating phase reports against the shared graph
                // must not see two sequences for one event.
                let _ = self.lifecycle.transition(LifecycleState::Stopping);
                // Rollback spends the same bounded shutdown budget a deliberate
                // stop would.
                let deadline = self.deadline(self.config.shutdown_timeout);
                let rollback_fault = self.stop_pass(deadline, self.config.shutdown_timeout);
                self.started = 0;
                let _ = self.lifecycle.transition(LifecycleState::Failed);
                let mut fault = start_fault.with_context("component", component_name);
                if let Some(rollback_fault) = rollback_fault {
                    fault = fault
                        .with_context("rollback_failed", true)
                        .with_context("rollback_code", rollback_fault.code().to_string());
                }
                return Err(fault);
            }
            self.started = index + 1;
        }
        self.lifecycle.transition(LifecycleState::Running)
    }
    /// Enter graceful drain and invoke every started component in reverse order.
    ///
    /// All components receive the drain hook even if an earlier hook fails. The
    /// first failure is returned after the reverse-order pass, which ends early
    /// only when the drain budget expires.
    ///
    /// A drain requested on its own — the pattern a composition root uses when
    /// it closes admission the moment SIGTERM arrives, rather than when its
    /// serve loop unwinds — spends its own `drain_timeout`. The later `stop`
    /// then spends `shutdown_timeout` from the moment it is called, so the two
    /// budgets add up. That is deliberate: the time between them is time the
    /// process spends serving established work, which is the point of draining
    /// early.
    pub fn drain(&mut self) -> FaultResult<()> {
        match self.lifecycle.state() {
            LifecycleState::Running => self.lifecycle.transition(LifecycleState::Draining)?,
            LifecycleState::Draining => {}
            LifecycleState::Stopped => return Ok(()),
            other @ (LifecycleState::Created
            | LifecycleState::Starting
            | LifecycleState::Stopping
            | LifecycleState::Failed) => {
                return Err(Fault::new(
                    Code::FailedPrecondition,
                    "service cannot drain from its current lifecycle state",
                )
                .with_context("service_state", other.as_str()));
            }
        }
        let deadline = self.deadline(self.config.drain_timeout);
        let budget = self.config.drain_timeout;
        self.drain_pass(deadline, budget).map_or(Ok(()), Err)
    }
    /// Stop every started component in reverse order.
    ///
    /// Shutdown is exhaustive: one component failure never prevents later
    /// components from receiving their stop hook. An expired shutdown budget
    /// does end the pass, because past that point the orchestrator is about to
    /// remove the process anyway and the remaining hooks would run into a
    /// SIGKILL. The Go coordinator ends its stop pass on the same condition.
    ///
    /// The drain phase inside `stop` is bounded by `drain_timeout` and the
    /// whole call by `shutdown_timeout`, so a slow drain cannot silently eat
    /// the stop pass — `ServiceConfig::validate` rejects a configuration that
    /// leaves the stop pass nothing. It can still be starved by a single drain
    /// hook that overruns its own budget, because a synchronous hook cannot be
    /// cancelled from here.
    pub fn stop(&mut self) -> FaultResult<()> {
        let began = self.clock.monotonic_now();
        let shutdown_deadline = add(began, self.config.shutdown_timeout);
        // Drain is a phase inside the shutdown budget, so its deadline is
        // clamped: a validated configuration keeps the two apart, and this
        // holds the line for a `Service::new` default if those ever converge.
        let drain_budget = self.config.drain_timeout.min(self.config.shutdown_timeout);
        let drain_deadline = add(began, drain_budget);

        let mut first_fault = None;
        if self.lifecycle.state() == LifecycleState::Running {
            self.lifecycle.transition(LifecycleState::Draining)?;
            first_fault = self.drain_pass(drain_deadline, drain_budget);
        }
        match self.lifecycle.state() {
            LifecycleState::Draining => self.lifecycle.transition(LifecycleState::Stopping)?,
            LifecycleState::Stopping => {}
            LifecycleState::Stopped => return first_fault.map_or(Ok(()), Err),
            LifecycleState::Failed => {
                // Failed startup already rolled back started components.
                self.started = 0;
                return first_fault.map_or(Ok(()), Err);
            }
            other @ (LifecycleState::Created
            | LifecycleState::Starting
            | LifecycleState::Running) => {
                return Err(Fault::new(
                    Code::FailedPrecondition,
                    "service cannot stop from its current lifecycle state",
                )
                .with_context("service_state", other.as_str()));
            }
        }
        let stop_fault = self.stop_pass(shutdown_deadline, self.config.shutdown_timeout);
        if first_fault.is_none() {
            first_fault = stop_fault;
        }
        self.started = 0;
        if first_fault.is_some() {
            let _ = self.lifecycle.transition(LifecycleState::Failed);
        } else {
            self.lifecycle.transition(LifecycleState::Stopped)?;
        }
        first_fault.map_or(Ok(()), Err)
    }
    #[must_use]
    pub fn lifecycle(&self) -> Lifecycle {
        self.lifecycle.clone()
    }
    /// The validated process name carried in lifecycle diagnostics.
    #[must_use]
    pub fn name(&self) -> &str {
        &self.config.name
    }
    fn deadline(&self, budget: Duration) -> Instant {
        add(self.clock.monotonic_now(), budget)
    }
    /// `budget` is the effective budget behind `deadline`, reported in the
    /// expiry fault so an operator is sent to the knob that was enforced.
    fn drain_pass(&mut self, deadline: Instant, budget: Duration) -> Option<Fault> {
        let clock = Arc::clone(&self.clock);
        let service = self.config.name.clone();
        let mut first_fault = None;
        for component in self.components[..self.started].iter_mut().rev() {
            if clock.monotonic_now() >= deadline {
                let expired = budget_expired(&service, "drain", component.name(), budget);
                if first_fault.is_none() {
                    first_fault = Some(expired);
                }
                break;
            }
            if let Err(fault) = component.drain()
                && first_fault.is_none()
            {
                first_fault = Some(fault.with_context("component", component.name().to_owned()));
            }
        }
        first_fault
    }
    fn stop_pass(&mut self, deadline: Instant, budget: Duration) -> Option<Fault> {
        let clock = Arc::clone(&self.clock);
        let service = self.config.name.clone();
        let mut first_fault = None;
        for component in self.components[..self.started].iter_mut().rev() {
            if clock.monotonic_now() >= deadline {
                let expired = budget_expired(&service, "stop", component.name(), budget);
                if first_fault.is_none() {
                    first_fault = Some(expired);
                }
                break;
            }
            if let Err(fault) = component.stop()
                && first_fault.is_none()
            {
                first_fault = Some(fault.with_context("component", component.name().to_owned()));
            }
        }
        first_fault
    }
}

/// A deadline that cannot be represented is already expired, so an
/// unrepresentable budget fails closed instead of becoming an infinite one.
fn add(instant: Instant, budget: Duration) -> Instant {
    instant.checked_add(budget).unwrap_or(instant)
}

fn budget_expired(service: &str, phase: &str, component: &str, budget: Duration) -> Fault {
    Fault::new(
        Code::DeadlineExceeded,
        "service lifecycle budget expired before every component was visited",
    )
    .with_context("service", service.to_owned())
    .with_context("phase", phase.to_owned())
    .with_context("component", component.to_owned())
    .with_context("timeout_millis", budget.as_millis().to_string())
}

impl Default for Service {
    fn default() -> Self {
        Self::new()
    }
}
