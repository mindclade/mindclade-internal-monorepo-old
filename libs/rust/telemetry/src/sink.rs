// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Event sink contracts.

use crate::Event;
use mindclade_faults::FaultResult;
use std::sync::{Arc, Mutex};

pub trait Sink: Send + Sync {
    fn emit(&self, event: &Event) -> FaultResult<()>;
    fn flush(&self) -> FaultResult<()> {
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Default)]
pub struct NoopSink;
impl Sink for NoopSink {
    fn emit(&self, _event: &Event) -> FaultResult<()> {
        Ok(())
    }
}

#[derive(Clone, Debug, Default)]
pub struct MemorySink {
    events: Arc<Mutex<Vec<Event>>>,
}
impl MemorySink {
    #[must_use]
    pub fn snapshot(&self) -> Vec<Event> {
        self.events
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .clone()
    }
}
impl Sink for MemorySink {
    fn emit(&self, event: &Event) -> FaultResult<()> {
        self.events
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .push(event.clone());
        Ok(())
    }
}

#[derive(Default)]
pub struct FanoutSink {
    sinks: Vec<Arc<dyn Sink>>,
}
impl core::fmt::Debug for FanoutSink {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("FanoutSink")
            .field("sink_count", &self.sinks.len())
            .finish()
    }
}
impl FanoutSink {
    #[must_use]
    pub fn new(sinks: Vec<Arc<dyn Sink>>) -> Self {
        Self { sinks }
    }
}
impl Sink for FanoutSink {
    fn emit(&self, event: &Event) -> FaultResult<()> {
        for sink in &self.sinks {
            sink.emit(event)?;
        }
        Ok(())
    }
    fn flush(&self) -> FaultResult<()> {
        for sink in &self.sinks {
            sink.flush()?;
        }
        Ok(())
    }
}
