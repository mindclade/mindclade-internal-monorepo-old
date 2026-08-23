// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Process-level lifecycle timeouts shared by Rust services.

use mindclade_faults::{Fault, FaultResult};
use std::time::Duration;

const MAX_SERVICE_NAME_BYTES: usize = 128;
const MAX_LIFECYCLE_TIMEOUT: Duration = Duration::from_mins(15);

/// Budget for the whole reverse-order drain pass.
///
/// The Go runtime's `defaultComponentDrainTimeout` is the same 10s but applies
/// per component, because Go can abandon a hook that ignores its deadline and
/// this crate cannot. Size a `terminationGracePeriodSeconds` from the budget
/// that is actually enforced rather than from the shared number.
pub const DEFAULT_DRAIN_TIMEOUT: Duration = Duration::from_secs(10);

/// Budget for the whole of `Service::stop`, drain pass included.
///
/// Matches the Go runtime's `defaultShutdownTimeout`, which bounds the same
/// span: drain, then stop, then the wait for run loops.
pub const DEFAULT_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ServiceConfig {
    pub name: String,
    pub drain_timeout: Duration,
    pub shutdown_timeout: Duration,
}

impl ServiceConfig {
    /// Named configuration with the repository-default lifecycle budgets.
    pub fn new(name: impl Into<String>) -> FaultResult<Self> {
        Self {
            name: name.into(),
            drain_timeout: DEFAULT_DRAIN_TIMEOUT,
            shutdown_timeout: DEFAULT_SHUTDOWN_TIMEOUT,
        }
        .validate()
    }
    /// Validate bounded service identity and lifecycle deadlines.
    pub fn validate(self) -> FaultResult<Self> {
        let name = self.name.trim();
        if name.is_empty() || name.len() > MAX_SERVICE_NAME_BYTES || name != self.name {
            return Err(Fault::invalid_argument("service name is invalid"));
        }
        for (field, timeout) in [
            ("drain_timeout", self.drain_timeout),
            ("shutdown_timeout", self.shutdown_timeout),
        ] {
            if timeout.is_zero() || timeout > MAX_LIFECYCLE_TIMEOUT {
                return Err(
                    Fault::invalid_argument("service lifecycle timeout is invalid")
                        .with_context("field", field)
                        .with_context("millis", timeout.as_millis().to_string()),
                );
            }
        }
        // Drain is a phase inside shutdown, not a budget beside it, and the
        // stop pass has to be left something to spend: a drain allowed to
        // consume the whole shutdown budget means no component ever receives
        // its stop hook. Equal budgets are rejected for that reason, not only
        // larger ones.
        if self.drain_timeout >= self.shutdown_timeout {
            return Err(Fault::invalid_argument(
                "drain timeout leaves no shutdown budget for the stop pass",
            )
            .with_context("drain_millis", self.drain_timeout.as_millis().to_string())
            .with_context(
                "shutdown_millis",
                self.shutdown_timeout.as_millis().to_string(),
            ));
        }
        Ok(self)
    }
}
