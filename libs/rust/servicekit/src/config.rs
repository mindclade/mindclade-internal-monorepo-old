//! Process-level lifecycle timeouts shared by Rust services.

use mindclade_faults::{Fault, FaultResult};
use std::time::Duration;

const MAX_SERVICE_NAME_BYTES: usize = 128;
const MAX_LIFECYCLE_TIMEOUT: Duration = Duration::from_secs(15 * 60);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ServiceConfig {
    pub name: String,
    pub drain_timeout: Duration,
    pub shutdown_timeout: Duration,
}

impl ServiceConfig {
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
                return Err(Fault::invalid_argument("service lifecycle timeout is invalid")
                    .with_context("field", field)
                    .with_context("millis", timeout.as_millis().to_string()));
            }
        }
        Ok(self)
    }
}
