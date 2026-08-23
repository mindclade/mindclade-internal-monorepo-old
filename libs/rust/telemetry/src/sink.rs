// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Event sink contracts and the in-tree writer implementation.

use crate::Event;
use crate::json;
use mindclade_faults::{Code, Fault, FaultResult};
use std::io::Write;
use std::sync::atomic::{AtomicU64, Ordering};
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

/// How a [`WriterSink`] relates `emit` to the underlying writer.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum FlushPolicy {
    /// Encode and write inside `emit`. The record is in the writer before
    /// `emit` returns, so a crash loses nothing already emitted — at the cost
    /// of a write syscall on the calling path. Correct for a low-rate control
    /// path or a file that must not lose its tail.
    PerEvent,
    /// Encode inside `emit` into a bounded staging buffer and write only on
    /// `flush`. No I/O on the hot path. The composition root owns the flush
    /// cadence and must call `flush` on a bounded interval and at shutdown.
    Deferred { staging_bytes: usize },
}

/// Writes events to any [`Write`] as newline-delimited JSON.
///
/// The first `Sink` in this tree that puts a record anywhere: `NoopSink`
/// discards, `MemorySink` is a test double, and `FanoutSink` only forwards to
/// other sinks, so a service that did not inject something out-of-tree emitted
/// into a `NoopSink`. Point this at `io::stdout()` for a container collector or
/// at a file for a node-local log.
///
/// # Foundation constraints
///
/// Nothing is installed and nothing is spawned. There is no global subscriber,
/// no static registry, and no background thread: the writer is passed in, and
/// draining is a call the composition root makes. `libs/rust/SECURITY.md`
/// requires that a foundation crate create no ambient runtime, global thread
/// pool, or hidden client, which is the reason a batteries-included telemetry
/// framework was declined for this tier.
///
/// # Boundedness
///
/// Under [`FlushPolicy::Deferred`] the staging buffer never exceeds
/// `staging_bytes`; an event that will not fit is dropped and counted by
/// [`Self::dropped`] rather than growing the buffer or blocking the caller.
/// A non-zero drop count means the flush cadence is too slow for the emit
/// rate, and is the signal to alarm on.
pub struct WriterSink<W: Write + Send> {
    inner: Mutex<Staging<W>>,
    policy: FlushPolicy,
    dropped: AtomicU64,
}

#[derive(Debug)]
struct Staging<W: Write + Send> {
    writer: W,
    staged: Vec<u8>,
}

/// Increments a drop counter without ever wrapping.
///
/// `fetch_add` would roll a saturated counter back to zero, and a drop counter
/// reading zero is indistinguishable from a sink that is losing nothing.
/// `checked_add` yields `None` at the ceiling, which leaves `fetch_update` a
/// no-op and pins the counter at `u64::MAX`.
fn count_drop(counter: &AtomicU64) {
    let _ = counter.fetch_update(Ordering::Relaxed, Ordering::Relaxed, |current| {
        current.checked_add(1)
    });
}

impl<W: Write + Send> WriterSink<W> {
    /// Default staging budget for [`Self::deferred`].
    pub const DEFAULT_STAGING_BYTES: usize = 1 << 20;

    /// Smallest staging budget accepted.
    ///
    /// A buffer that cannot hold one maximal record would drop every event
    /// that happened to be large, permanently and silently — the sink would
    /// look healthy and lose exactly the biggest records. Requiring room for
    /// one worst-case record makes "a freshly flushed buffer always admits the
    /// next event" an invariant rather than a hope.
    pub const MINIMUM_STAGING_BYTES: usize = json::MAX_RECORD_BYTES + 1;

    /// Writes each event through to `writer` inside `emit`.
    pub fn write_through(writer: W) -> Self {
        Self {
            inner: Mutex::new(Staging {
                writer,
                staged: Vec::new(),
            }),
            policy: FlushPolicy::PerEvent,
            dropped: AtomicU64::new(0),
        }
    }

    /// Stages events in a bounded buffer, writing them on [`Sink::flush`].
    pub fn deferred(writer: W, staging_bytes: usize) -> FaultResult<Self> {
        if staging_bytes < Self::MINIMUM_STAGING_BYTES {
            return Err(Fault::invalid_argument(
                "telemetry writer staging budget is below one maximal record",
            ));
        }
        Ok(Self {
            inner: Mutex::new(Staging {
                writer,
                staged: Vec::with_capacity(staging_bytes),
            }),
            policy: FlushPolicy::Deferred { staging_bytes },
            dropped: AtomicU64::new(0),
        })
    }

    /// Events discarded because the staging buffer was full.
    #[must_use]
    pub fn dropped(&self) -> u64 {
        self.dropped.load(Ordering::Relaxed)
    }

    /// Bytes currently staged and not yet written.
    #[must_use]
    pub fn staged_bytes(&self) -> usize {
        self.inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .staged
            .len()
    }

    /// Recovers the writer, flushing anything still staged.
    pub fn into_inner(self) -> FaultResult<W> {
        self.flush()?;
        Ok(self
            .inner
            .into_inner()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .writer)
    }
}

impl<W: Write + Send> core::fmt::Debug for WriterSink<W> {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("WriterSink")
            .field("policy", &self.policy)
            .field("dropped", &self.dropped())
            .finish_non_exhaustive()
    }
}

impl<W: Write + Send> Sink for WriterSink<W> {
    fn emit(&self, event: &Event) -> FaultResult<()> {
        let mut line = json::encode_event(event)?;
        line.push('\n');
        let mut staging = self
            .inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        match self.policy {
            FlushPolicy::PerEvent => {
                staging.writer.write_all(line.as_bytes()).map_err(|error| {
                    Fault::internal("failed to write telemetry record").with_source(error)
                })?;
                staging.writer.flush().map_err(|error| {
                    Fault::internal("failed to flush telemetry writer").with_source(error)
                })
            }
            FlushPolicy::Deferred { staging_bytes } => {
                // A sum that would overflow `usize` is by definition past the
                // budget, so `None` and "too large" take the same branch.
                let admissible = staging
                    .staged
                    .len()
                    .checked_add(line.len())
                    .is_some_and(|projected| projected <= staging_bytes);
                if admissible {
                    staging.staged.extend_from_slice(line.as_bytes());
                } else {
                    count_drop(&self.dropped);
                }
                Ok(())
            }
        }
    }

    fn flush(&self) -> FaultResult<()> {
        let mut staging = self
            .inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if !staging.staged.is_empty() {
            // Move the staged bytes out before writing so a failed write does
            // not leave the buffer permanently full: a wedged buffer would
            // convert one transient I/O error into a sink that drops every
            // subsequent event for the life of the process.
            let pending = std::mem::take(&mut staging.staged);
            let result = staging.writer.write_all(&pending);
            staging.staged = pending;
            staging.staged.clear();
            result.map_err(|error| {
                Fault::new(
                    Code::Unavailable,
                    "failed to write staged telemetry records",
                )
                .with_source(error)
            })?;
        }
        staging
            .writer
            .flush()
            .map_err(|error| Fault::internal("failed to flush telemetry writer").with_source(error))
    }
}
