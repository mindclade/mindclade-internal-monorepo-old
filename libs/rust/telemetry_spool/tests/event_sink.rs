// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The spool had no consumer and `telemetry` had no durable path; the two
//! crates shared no type and no dependency edge. These tests cover the join:
//! an `Event` emitted through a `Sink` reaches a segment on disk, is replayed
//! and decoded back into an equal `Event`, and a spool at its disk budget
//! degrades into counted drops rather than wedging the emitting process.

use mindclade_bytes_io::ByteSize;
use mindclade_faults::Code;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::{Clock, ManualClock};
use mindclade_telemetry::{Attributes, Event, Severity, Sink, TraceContext};
use mindclade_telemetry_spool::{
    EVENT_TYPE, SpoolConfig, SpoolSink, TelemetrySpool, decode_event, delivery,
};
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant, UNIX_EPOCH};

fn scratch(name: &str) -> PathBuf {
    let root = std::env::temp_dir().join(format!(
        "mindclade-spool-sink-{}-{name}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&root);
    root
}

fn clock() -> Arc<ManualClock> {
    Arc::new(ManualClock::new(
        // Whole milliseconds: the spool envelope and this payload both carry
        // Unix milliseconds, so a sub-millisecond wall clock would not round
        // trip and the test would be asserting the truncation, not the codec.
        UNIX_EPOCH + Duration::from_millis(1_774_000_000_123),
        Instant::now(),
    ))
}

fn sample_event(clock: &dyn Clock) -> Event {
    let mut attributes = Attributes::new();
    assert!(attributes.insert("step", 42_u64));
    assert!(attributes.insert("delta", -7_i64));
    assert!(attributes.insert("ratio", 0.25_f64));
    assert!(attributes.insert("ok", true));
    assert!(attributes.insert("note", "unicode ✓ and \"quotes\""));
    assert!(attributes.insert_redacted("token"));
    let mut event = Event::new("checkpoint.committed", Severity::Warn, clock)
        .expect("event identifier generates");
    event.trace = Some(TraceContext {
        trace_id: "4bf92f3577b34da6a3ce929d0e0e4736".to_owned(),
        span_id: "00f067aa0ba902b7".to_owned(),
        sampled: true,
    });
    event.attributes = attributes;
    event
}

fn spool_bytes(root: &Path) -> u64 {
    fs::read_dir(root)
        .expect("spool root is readable")
        .filter_map(Result::ok)
        .filter(|entry| entry.file_name().to_string_lossy().starts_with("segment-"))
        .filter_map(|entry| entry.metadata().ok())
        .map(|metadata| metadata.len())
        .sum()
}

#[test]
fn an_event_emitted_through_the_sink_survives_the_durable_round_trip() {
    let root = scratch("round-trip");
    let clock = clock();
    let spool = Arc::new(
        TelemetrySpool::open(&root, SpoolConfig::default(), clock.clone()).expect("spool opens"),
    );
    let sink = SpoolSink::new(Arc::clone(&spool));
    let event = sample_event(clock.as_ref());

    sink.emit(&event).expect("emit appends to the spool");
    // `append` fsyncs before returning, so there is no buffered tail; flush
    // exists to satisfy the trait, not to force anything out.
    sink.flush().expect("flush is a no-op");

    // Read back through the spool's own replay path, exactly as a forwarder
    // would after a restart.
    let replayed = spool.replay_after(0, 10).expect("replay succeeds");
    assert_eq!(replayed.len(), 1);
    let envelope = &replayed[0];
    assert_eq!(envelope.event_type, EVENT_TYPE);
    assert_eq!(envelope.sequence, 1);
    assert_eq!(envelope.timestamp_millis, 1_774_000_000_123);

    let decoded = decode_event(&envelope.payload).expect("payload decodes");
    assert_eq!(
        decoded, event,
        "the event must survive the round trip intact"
    );
    // The redaction is preserved as a redaction rather than rehydrated into a
    // value, which is the property that makes spooling a redacted event safe.
    assert!(
        decoded.attributes.iter().any(|(key, value)| key == "token"
            && *value == mindclade_telemetry::AttributeValue::Redacted)
    );

    let _ = fs::remove_dir_all(&root);
}

#[test]
fn a_corrupt_payload_is_rejected_rather_than_decoded() {
    // A segment is a file on a node's disk that a forwarder reads back after a
    // crash. Whoever wrote it, the decoder treats it as untrusted.
    assert!(decode_event(&[]).is_err());
    assert!(decode_event(&[0xff; 64]).is_err());

    let clock = clock();
    let mut payload = mindclade_telemetry_spool::encode_event(&sample_event(clock.as_ref()))
        .expect("encode succeeds");
    payload.push(0);
    assert!(
        decode_event(&payload).is_err(),
        "trailing bytes must not be ignored"
    );
}

#[test]
fn an_event_id_of_the_wrong_kind_is_refused() {
    let clock = clock();
    let mut event = sample_event(clock.as_ref());
    event.event_id = ResourceId::generate("run", clock.as_ref()).expect("identifier generates");
    assert!(mindclade_telemetry_spool::encode_event(&event).is_err());
}

#[test]
fn a_full_spool_degrades_into_counted_drops_and_recovers() {
    let root = scratch("budget");
    let clock = clock();
    let config = SpoolConfig {
        maximum_event_bytes: ByteSize::new(1024),
        maximum_segment_bytes: ByteSize::new(4096),
        maximum_total_bytes: ByteSize::new(8192),
    };
    let spool = Arc::new(TelemetrySpool::open(&root, config, clock.clone()).expect("spool opens"));
    let sink = SpoolSink::new(Arc::clone(&spool));
    let event = sample_event(clock.as_ref());

    // Emit far past the disk budget. The invariant under test is that the
    // emitting path never fails and never grows: a telemetry disk that filled
    // up must not take request serving down with it.
    for _ in 0..500 {
        sink.emit(&event)
            .expect("a full spool must not fail the emitting path");
    }
    assert!(
        sink.dropped() > 0,
        "exceeding the disk budget must be counted, not silent"
    );
    assert!(
        spool_bytes(&root) <= 8192,
        "the spool must never exceed its configured total budget"
    );

    // The spool rejects on its own too, so the sink is absorbing exactly that
    // condition and nothing else. Sized at the configured per-event maximum:
    // the loop above stopped when the remaining budget could no longer hold
    // one of its own records, which is strictly smaller than this.
    let direct = spool
        .append(EVENT_TYPE, &vec![0_u8; 1024])
        .expect_err("a full spool rejects a maximal event");
    assert_eq!(direct.code(), Code::ResourceExhausted);

    // Drain, acknowledge, and compact — the loop a composition root owns —
    // then prove the spool accepts writes again. A spool that stayed wedged
    // after being drained would be a permanent outage rather than backpressure.
    let dropped_while_full = sink.dropped();
    let delivered = drain(&spool);
    assert!(delivered > 0, "replay must return the spooled events");
    assert!(spool.compact().expect("compaction succeeds") > 0);

    sink.emit(&event).expect("emit after compaction");
    assert_eq!(
        sink.dropped(),
        dropped_while_full,
        "a compacted spool must admit new events again"
    );

    let _ = fs::remove_dir_all(&root);
}

/// Replays every spooled event in bounded batches, decoding each payload and
/// acknowledging the batch, the way a forwarder would.
fn drain(spool: &Arc<TelemetrySpool>) -> usize {
    struct Counting(std::cell::Cell<usize>);
    impl delivery::BatchSink for Counting {
        fn deliver(
            &self,
            batch: &mindclade_telemetry_spool::DeliveryBatch,
        ) -> mindclade_faults::FaultResult<()> {
            for envelope in &batch.envelopes {
                // Every spooled payload must decode; a forwarder that skipped
                // an undecodable record would acknowledge data loss.
                decode_event(&envelope.payload)?;
            }
            self.0.set(self.0.get() + batch.envelopes.len());
            Ok(())
        }
    }
    let counting = Counting(std::cell::Cell::new(0));
    let mut after = 0_u64;
    // Bounded loop: each pass acknowledges strictly forward, and the spool's
    // own total budget caps how many passes can be required.
    for _ in 0..64 {
        match delivery::deliver_after(spool, after, 100, 1 << 20, &counting) {
            Ok(Some(highest)) => after = highest,
            Ok(None) => break,
            Err(error) => panic!("delivery failed: {error}"),
        }
    }
    counting.0.get()
}

#[test]
fn the_spool_clock_and_the_payload_agree_on_the_timestamp() {
    let root = scratch("timestamp");
    let clock = clock();
    let spool = Arc::new(
        TelemetrySpool::open(&root, SpoolConfig::default(), clock.clone()).expect("spool opens"),
    );
    let sink = SpoolSink::new(Arc::clone(&spool));
    let event = sample_event(clock.as_ref());
    sink.emit(&event).expect("emit succeeds");

    let replayed = spool.replay_after(0, 1).expect("replay succeeds");
    let decoded = decode_event(&replayed[0].payload).expect("payload decodes");
    // The envelope and its payload carry the same instant. Storing seconds in
    // one and milliseconds in the other is how the two silently drift.
    assert_eq!(
        decoded.timestamp,
        UNIX_EPOCH + Duration::from_millis(replayed[0].timestamp_millis)
    );
    assert_eq!(decoded.timestamp, event.timestamp);

    let _ = fs::remove_dir_all(&root);
}
