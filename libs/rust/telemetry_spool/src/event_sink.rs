// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! A [`mindclade_telemetry::Sink`] backed by the durable spool.
//!
//! The spool had no consumer: it implemented segmented durable persistence,
//! byte budgets, replay watermarks and compaction, and nothing in the tree
//! appended to it. `telemetry` had the opposite gap — a `Sink` trait whose
//! only implementations discarded, buffered in memory, or forwarded to other
//! sinks. This is the join.
//!
//! # Layering
//!
//! Lives here, not in `telemetry`. `telemetry` is Layer 1 and this crate is
//! Layer 3; `libs/rust/LAYERS.md` makes production dependencies
//! downward-only, so the crate that may name both types is this one.
//!
//! # Composition
//!
//! Nothing is spawned. Draining is the composition root's job and is already
//! written: [`crate::delivery::deliver_after`] reads a bounded batch, hands it
//! to a `BatchSink`, and acknowledges the highest delivered sequence, after
//! which [`crate::TelemetrySpool::compact`] reclaims fully acknowledged
//! segments. Run that from the process's own supervised task on a bounded
//! cadence, and call it once more inside the shutdown budget.

use crate::TelemetrySpool;
use crate::event_codec::{EVENT_TYPE, encode_event};
use mindclade_faults::{Code, FaultResult};
use mindclade_telemetry::{Event, Sink};
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

/// Appends every emitted event to a durable spool.
///
/// # Durability
///
/// [`crate::TelemetrySpool::append`] fsyncs the segment before it returns, so
/// an event that `emit` accepted is on disk when `emit` returns and survives
/// process death. [`Sink::flush`] is consequently a no-op rather than an
/// omission — there is no buffered tail to force out.
///
/// # Degradation
///
/// The spool enforces a total disk budget and rejects an append that would
/// breach it. That rejection is a capacity condition, not a caller error, and
/// it lasts until a forwarder acknowledges and compacts. Failing the emitting
/// operation for it would let a full telemetry disk take down request serving,
/// so a `ResourceExhausted` append is counted in [`Self::dropped`] and `emit`
/// returns `Ok`. Every other fault propagates: a malformed event or a broken
/// filesystem is not something to absorb.
///
/// A non-zero drop count is the alarm. Publish it as a counter — it says the
/// forwarder is not keeping up, or is not running at all.
pub struct SpoolSink {
    spool: Arc<TelemetrySpool>,
    dropped: AtomicU64,
}

impl SpoolSink {
    #[must_use]
    pub fn new(spool: Arc<TelemetrySpool>) -> Self {
        Self {
            spool,
            dropped: AtomicU64::new(0),
        }
    }

    /// Events discarded because the spool's disk budget was exhausted.
    #[must_use]
    pub fn dropped(&self) -> u64 {
        self.dropped.load(Ordering::Relaxed)
    }

    /// The underlying spool, for the drain and compaction loop.
    #[must_use]
    pub fn spool(&self) -> &Arc<TelemetrySpool> {
        &self.spool
    }
}

impl core::fmt::Debug for SpoolSink {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("SpoolSink")
            .field("dropped", &self.dropped())
            .finish_non_exhaustive()
    }
}

impl Sink for SpoolSink {
    fn emit(&self, event: &Event) -> FaultResult<()> {
        let payload = encode_event(event)?;
        match self.spool.append(EVENT_TYPE, &payload) {
            Ok(_) => Ok(()),
            Err(fault) if fault.code() == Code::ResourceExhausted => {
                // Relaxed is sufficient: this counter is read for reporting,
                // never to order anything against the append itself.
                // `checked_add` rather than `fetch_add`, because a drop counter
                // that wrapped back to zero would read as a sink losing
                // nothing; `None` at the ceiling leaves it pinned at u64::MAX.
                let _ =
                    self.dropped
                        .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |current| {
                            current.checked_add(1)
                        });
                Ok(())
            }
            Err(fault) => Err(fault),
        }
    }

    fn flush(&self) -> FaultResult<()> {
        Ok(())
    }
}
