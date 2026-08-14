//! Structured event envelope.

use crate::Attributes;
use mindclade_runtime_core::Clock;
use mindclade_identifiers::ResourceId;
use std::time::SystemTime;

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum Severity { Trace, Debug, Info, Warn, Error, Critical }

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TraceContext {
    pub trace_id: String,
    pub span_id: String,
    pub sampled: bool,
}

#[derive(Clone, Debug)]
pub struct Event {
    pub event_id: ResourceId,
    pub name: String,
    pub severity: Severity,
    pub timestamp: SystemTime,
    pub trace: Option<TraceContext>,
    pub attributes: Attributes,
}

impl Event {
    pub fn new(name: impl Into<String>, severity: Severity, clock: &dyn Clock) -> Result<Self, mindclade_identifiers::ResourceIdError> {
        Ok(Self {
            event_id: ResourceId::generate("evt", clock)?,
            name: name.into(),
            severity,
            timestamp: clock.system_now(),
            trace: None,
            attributes: Attributes::new(),
        })
    }
}
