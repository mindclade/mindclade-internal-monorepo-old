// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Structured event envelope.

use crate::Attributes;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::Clock;
use std::time::SystemTime;

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum Severity {
    Trace,
    Debug,
    Info,
    Warn,
    Error,
    Critical,
}

impl Severity {
    /// Rendering used in the `level` member of a structured record.
    ///
    /// Uppercase because the Go tier logs through `log/slog`, whose JSON
    /// handler writes `"level":"INFO"`. One log pipeline should not need two
    /// severity vocabularies to tell a Go warning from a Rust one. `TRACE` and
    /// `CRITICAL` have no `slog` constant and render as `slog` would print an
    /// offset level (`DEBUG-4`, `ERROR+4`) — spelling them out is both
    /// readable and unambiguous, and the ordering is preserved either way.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Trace => "TRACE",
            Self::Debug => "DEBUG",
            Self::Info => "INFO",
            Self::Warn => "WARN",
            Self::Error => "ERROR",
            Self::Critical => "CRITICAL",
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TraceContext {
    pub trace_id: String,
    pub span_id: String,
    pub sampled: bool,
}

impl TraceContext {
    /// W3C trace-context shape: a 32-hex trace ID and a 16-hex span ID.
    ///
    /// `libs/go/observability.TraceContext.Validate` enforces exactly this and
    /// its `Attributes()` yields nothing when it fails, so an ill-formed trace
    /// context is omitted from a record rather than exported as a broken
    /// correlation key that no collector can join on.
    #[must_use]
    pub fn is_valid(&self) -> bool {
        self.trace_id.len() == 32
            && self.span_id.len() == 16
            && self.trace_id.bytes().all(|byte| byte.is_ascii_hexdigit())
            && self.span_id.bytes().all(|byte| byte.is_ascii_hexdigit())
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Event {
    pub event_id: ResourceId,
    pub name: String,
    pub severity: Severity,
    pub timestamp: SystemTime,
    pub trace: Option<TraceContext>,
    pub attributes: Attributes,
}

impl Event {
    /// Maximum event-name length accepted at a persistence or export boundary.
    ///
    /// Matches the bound `SpanContext::new` already applies to a span name, so
    /// a span and the event that reports it cannot disagree about what fits.
    pub const MAX_NAME_LEN: usize = 256;

    pub fn new(
        name: impl Into<String>,
        severity: Severity,
        clock: &dyn Clock,
    ) -> Result<Self, mindclade_identifiers::ResourceIdError> {
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
