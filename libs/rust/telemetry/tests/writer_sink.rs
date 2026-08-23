// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The writer sink is the first `Sink` in this tree that puts a record
//! anywhere. Before it, a service that did not inject an out-of-tree sink
//! emitted into `NoopSink` and its telemetry went nowhere, which is what these
//! tests exist to make impossible to reintroduce silently.

use mindclade_identifiers::ResourceId;
use mindclade_telemetry::{
    Attributes, Event, NoopSink, Severity, Sink, TraceContext, WriterSink, json,
};
use std::fs;
use std::io::Write;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::{Duration, UNIX_EPOCH};

/// A fixed event, so a rendered record can be asserted byte for byte.
fn sample_event() -> Event {
    let mut attributes = Attributes::new();
    assert!(attributes.insert("a.step", 42_u64));
    assert!(attributes.insert("b.ratio", 0.5_f64));
    assert!(attributes.insert("c.note", "line\none\t\"quoted\" \u{1}"));
    assert!(attributes.insert("d.ok", true));
    assert!(attributes.insert_redacted("e.token"));
    Event {
        event_id: ResourceId::parse("evt_0193b8f0c1d27abc8def0123456789ab")
            .expect("fixed identifier parses"),
        name: "checkpoint.committed".to_owned(),
        severity: Severity::Info,
        timestamp: UNIX_EPOCH + Duration::from_millis(1_774_000_000_123),
        trace: Some(TraceContext {
            trace_id: "4BF92F3577B34DA6A3CE929D0E0E4736".to_owned(),
            span_id: "00F067AA0BA902B7".to_owned(),
            sampled: true,
        }),
        attributes,
    }
}

const EXPECTED_LINE: &str = concat!(
    r#"{"time":"2026-03-20T09:46:40.123Z","level":"INFO","msg":"checkpoint.committed","#,
    r#""event.id":"evt_0193b8f0c1d27abc8def0123456789ab","#,
    r#""trace.id":"4bf92f3577b34da6a3ce929d0e0e4736","span.id":"00f067aa0ba902b7","trace.sampled":true,"#,
    r#""a.step":42,"b.ratio":0.5,"c.note":"line\none\t\"quoted\" \u0001","d.ok":true,"#,
    r#""e.token":"[REDACTED]"}"#,
);

fn scratch(name: &str) -> PathBuf {
    let path = std::env::temp_dir().join(format!(
        "mindclade-writer-sink-{}-{name}",
        std::process::id()
    ));
    let _ = fs::remove_file(&path);
    path
}

#[test]
fn write_through_lands_the_record_in_its_destination() {
    let path = scratch("write-through");
    let file = fs::File::create(&path).expect("create sink destination");
    let sink = WriterSink::write_through(file);
    let event = sample_event();

    // The event is on disk when emit returns; no flush, no drain loop.
    sink.emit(&event).expect("emit succeeds");
    let contents = fs::read_to_string(&path).expect("destination is readable");

    assert_eq!(contents, format!("{EXPECTED_LINE}\n"));
    // Same input, same bytes: the record is deterministic, so a golden line is
    // a usable assertion rather than a flaky one.
    sink.emit(&event).expect("second emit succeeds");
    let contents = fs::read_to_string(&path).expect("destination is readable");
    assert_eq!(contents, format!("{EXPECTED_LINE}\n{EXPECTED_LINE}\n"));

    // The contrast this sink exists to remove: `NoopSink` accepts the same
    // event and offers no destination to inspect, and it was the whole of the
    // in-tree production sink surface before this change.
    NoopSink.emit(&event).expect("noop accepts the event");

    let _ = fs::remove_file(&path);
}

#[test]
fn every_encoded_record_is_one_line() {
    // The attribute values carry a newline and a tab. If either reached the
    // destination raw, one record would arrive at a newline-delimited reader
    // as two, the second of them malformed.
    let line = json::encode_event(&sample_event()).expect("encode succeeds");
    assert!(!line.contains('\n'));
    assert!(!line.contains('\t'));
    assert!(line.contains(r"\n"));
    assert!(line.contains(r"\t"));
    assert!(line.contains(r"\u0001"));
}

#[test]
fn a_malformed_trace_context_is_omitted_rather_than_exported() {
    let mut event = sample_event();
    event.trace = Some(TraceContext {
        trace_id: "not-a-trace-id".to_owned(),
        span_id: "short".to_owned(),
        sampled: true,
    });
    let line = json::encode_event(&event).expect("encode succeeds");
    assert!(!line.contains("trace.id"));
    assert!(!line.contains("not-a-trace-id"));
}

#[test]
fn deferred_staging_is_bounded_and_drops_rather_than_growing() {
    let shared = Arc::new(Mutex::new(Vec::<u8>::new()));
    let budget = WriterSink::<SharedBuffer>::MINIMUM_STAGING_BYTES;
    let sink = WriterSink::deferred(SharedBuffer(Arc::clone(&shared)), budget)
        .expect("a budget at the minimum is accepted");
    let event = sample_event();

    // Nothing reaches the destination before the composition root flushes.
    sink.emit(&event).expect("emit stages");
    assert!(shared.lock().expect("lock").is_empty());
    assert_eq!(sink.staged_bytes(), EXPECTED_LINE.len() + 1);

    // Fill the budget. Emit never blocks, never grows the buffer, and never
    // fails; the overflow is counted instead.
    let record_bytes = EXPECTED_LINE.len() + 1;
    let capacity = budget / record_bytes;
    for _ in 0..=capacity {
        sink.emit(&event)
            .expect("emit never fails on a full buffer");
    }
    assert!(sink.staged_bytes() <= budget);
    assert!(sink.dropped() > 0, "a full staging buffer must count drops");

    // A flush drains everything staged and re-admits the next event, so a full
    // buffer is a backlog rather than a wedge.
    let dropped_before = sink.dropped();
    sink.flush().expect("flush drains");
    assert_eq!(sink.staged_bytes(), 0);
    assert_eq!(shared.lock().expect("lock").len(), capacity * record_bytes);
    sink.emit(&event).expect("emit after flush");
    assert_eq!(sink.dropped(), dropped_before);
    assert_eq!(sink.staged_bytes(), record_bytes);
}

#[test]
fn a_staging_budget_below_one_maximal_record_is_refused() {
    // Accepting it would produce a sink that silently loses exactly the
    // largest events, forever, while reporting no error.
    let too_small = WriterSink::<SharedBuffer>::MINIMUM_STAGING_BYTES - 1;
    let sink = WriterSink::deferred(SharedBuffer(Arc::new(Mutex::new(Vec::new()))), too_small);
    assert!(sink.is_err());
    assert!(json::MAX_RECORD_BYTES > Attributes::MAX_ATTRIBUTES * Attributes::MAX_STRING_LEN);
}

/// A writer shared with the test so the destination can be inspected while the
/// sink still owns its handle.
struct SharedBuffer(Arc<Mutex<Vec<u8>>>);

impl Write for SharedBuffer {
    fn write(&mut self, buffer: &[u8]) -> std::io::Result<usize> {
        self.0
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .extend_from_slice(buffer);
        Ok(buffer.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}
