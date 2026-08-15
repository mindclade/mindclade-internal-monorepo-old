// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded structured observability contracts and dependency-neutral adapters.
#![forbid(unsafe_code)]
mod attributes;
mod event;
pub mod logging;
pub mod metrics;
pub mod propagation;
mod sink;
pub mod tracing;
pub use attributes::{AttributeValue, Attributes};
pub use event::{Event, Severity, TraceContext};
pub use logging::Logger;
pub use metrics::CounterRegistry;
pub use sink::{FanoutSink, MemorySink, NoopSink, Sink};
pub use tracing::SpanContext;
