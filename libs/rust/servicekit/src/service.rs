// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Ordered component assembly with transactional startup and exhaustive shutdown.

use crate::{Component, Lifecycle, LifecycleState};
use mindclade_faults::{Code, Fault, FaultResult};

/// Deterministic process lifecycle owner.
///
/// Components start in registration order and drain/stop in reverse order.
/// Startup is transactional: if component `N` fails to start, all previously
/// started components are stopped before the startup error is returned.
pub struct Service {
    lifecycle: Lifecycle,
    components: Vec<Box<dyn Component>>,
    started: usize,
}

impl core::fmt::Debug for Service {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("Service")
            .field("state", &self.lifecycle.state())
            .field("components", &self.components.len())
            .field("started", &self.started)
            .finish()
    }
}

impl Service {
    #[must_use]
    pub fn new() -> Self {
        Self {
            lifecycle: Lifecycle::new(),
            components: Vec::new(),
            started: 0,
        }
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
                let rollback_fault = self.rollback_started();
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
    /// first failure is returned after the complete reverse-order pass.
    pub fn drain(&mut self) -> FaultResult<()> {
        match self.lifecycle.state() {
            LifecycleState::Running => self.lifecycle.transition(LifecycleState::Draining)?,
            LifecycleState::Draining => {}
            LifecycleState::Stopped => return Ok(()),
            other => {
                return Err(Fault::new(
                    Code::FailedPrecondition,
                    "service cannot drain from its current lifecycle state",
                )
                .with_context("state", format!("{other:?}")));
            }
        }
        let mut first_fault = None;
        for component in self.components[..self.started].iter_mut().rev() {
            if let Err(fault) = component.drain()
                && first_fault.is_none()
            {
                first_fault = Some(fault.with_context("component", component.name().to_owned()));
            }
        }
        match first_fault {
            Some(fault) => Err(fault),
            None => Ok(()),
        }
    }
    /// Stop every started component in reverse order.
    ///
    /// Shutdown is exhaustive: one component failure never prevents later
    /// components from receiving their stop hook.
    pub fn stop(&mut self) -> FaultResult<()> {
        let mut first_fault = None;
        if self.lifecycle.state() == LifecycleState::Running
            && let Err(fault) = self.drain()
        {
            first_fault = Some(fault);
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
            other => {
                return Err(Fault::new(
                    Code::FailedPrecondition,
                    "service cannot stop from its current lifecycle state",
                )
                .with_context("state", format!("{other:?}")));
            }
        }
        for component in self.components[..self.started].iter_mut().rev() {
            if let Err(fault) = component.stop()
                && first_fault.is_none()
            {
                first_fault = Some(fault.with_context("component", component.name().to_owned()));
            }
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
    fn rollback_started(&mut self) -> Option<Fault> {
        let mut first_fault = None;
        for component in self.components[..self.started].iter_mut().rev() {
            if let Err(fault) = component.stop()
                && first_fault.is_none()
            {
                first_fault = Some(fault.with_context("component", component.name().to_owned()));
            }
        }
        self.started = 0;
        first_fault
    }
}

impl Default for Service {
    fn default() -> Self {
        Self::new()
    }
}
