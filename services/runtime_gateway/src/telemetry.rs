// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Minimal bounded runtime-gateway telemetry adapter.

use mindclade_faults::FaultResult;
use mindclade_runtime_core::Clock;
use mindclade_telemetry::{Event, Severity, Sink};
use std::sync::Arc;

pub struct GatewayTelemetry {
    clock: Arc<dyn Clock>,
    sink: Arc<dyn Sink>,
}
impl core::fmt::Debug for GatewayTelemetry {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("GatewayTelemetry")
            .finish_non_exhaustive()
    }
}
impl GatewayTelemetry {
    #[must_use]
    pub fn new(clock: Arc<dyn Clock>, sink: Arc<dyn Sink>) -> Self {
        Self { clock, sink }
    }
    pub fn emit(&self, name: &str, severity: Severity) -> FaultResult<()> {
        let event = Event::new(name, severity, self.clock.as_ref()).map_err(|error| {
            mindclade_faults::Fault::internal("failed to allocate telemetry event id")
                .with_source(error)
        })?;
        self.sink.emit(&event)
    }
    pub fn flush(&self) -> FaultResult<()> {
        self.sink.flush()
    }
}
