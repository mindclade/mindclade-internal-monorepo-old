// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded structured observability contracts and dependency-neutral adapters.
#![forbid(unsafe_code)]
mod attributes;
mod event;
pub mod json;
pub mod logging;
pub mod metrics;
pub mod propagation;
mod sink;
pub mod tracing;
pub use attributes::{AttributeValue, Attributes, REDACTED_TEXT};
pub use event::{Event, Severity, TraceContext};
pub use logging::Logger;
pub use metrics::{
    COUNTER_SUFFIX, CounterRegistry, METRIC_NAMESPACE, PROMETHEUS_CONTENT_TYPE,
    prometheus_series_name, valid_counter_name,
};
pub use sink::{FanoutSink, FlushPolicy, MemorySink, NoopSink, Sink, WriterSink};
pub use tracing::SpanContext;
