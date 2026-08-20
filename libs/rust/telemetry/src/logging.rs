// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{Event, Severity, Sink};
use mindclade_faults::FaultResult;
use mindclade_runtime_core::Clock;
use std::sync::Arc;

#[derive(Clone)]
pub struct Logger {
    clock: Arc<dyn Clock>,
    sink: Arc<dyn Sink>,
}

impl core::fmt::Debug for Logger {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter.debug_struct("Logger").finish_non_exhaustive()
    }
}

impl Logger {
    #[must_use]
    pub fn new(clock: Arc<dyn Clock>, sink: Arc<dyn Sink>) -> Self {
        Self { clock, sink }
    }
    pub fn log(
        &self,
        name: &str,
        severity: Severity,
        fields: impl IntoIterator<Item = (String, String)>,
    ) -> FaultResult<()> {
        let mut e = Event::new(name, severity, self.clock.as_ref()).map_err(|x| {
            mindclade_faults::Fault::internal("failed to create telemetry event").with_source(x)
        })?;
        for (k, v) in fields {
            e.attributes.insert(k, v);
        }
        self.sink.emit(&e)
    }
}
