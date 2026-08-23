// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Wire format for a [`Event`] carried as a spool [`Envelope`] payload.
//!
//! Until now `Envelope.payload` was opaque bytes with no declared producer:
//! the spool and `mindclade_telemetry` shared no type and no dependency edge,
//! so nothing said what a spooled telemetry record contained. This module is
//! that declaration, and it lives here rather than in `telemetry` because
//! `telemetry` is Layer 1 and this crate is Layer 3 — `libs/rust/LAYERS.md`
//! makes dependencies downward-only, so the adapter belongs on the side that
//! may name both.
//!
//! # Format
//!
//! Length-delimited via `mindclade_record_io`, the same primitive `Envelope`
//! itself uses:
//!
//! ```text
//! u16   schema                     (EVENT_PAYLOAD_SCHEMA)
//! str   event_id                   ("evt_" + 32 hex)
//! str   name                       (1..=Event::MAX_NAME_LEN bytes)
//! u8    severity                    (SEVERITY_* below; 0 is never emitted)
//! u64   timestamp_millis           (Unix epoch)
//! bool  trace_present
//!   str trace_id / str span_id / bool sampled   (only when present)
//! u32   attribute_count            (<= Attributes::MAX_ATTRIBUTES)
//!   str key / u8 value_tag / value (repeated)
//! ```
//!
//! Every field is bounded and every bound is re-checked on decode. A spool
//! segment is a file on a node's disk that a forwarder reads back after a
//! crash, so a decoder must treat it as untrusted input regardless of who
//! wrote it.
//!
//! # Timestamps
//!
//! `Event.timestamp` is a `SystemTime`; the wire carries Unix milliseconds,
//! matching `Envelope.timestamp_millis` so the payload and its envelope cannot
//! disagree. Sub-millisecond precision does not survive the round trip. That
//! is deliberate — a second resolution in the payload would make the two
//! fields drift apart for the same event.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_record_io::{Decoder, Encoder};
use mindclade_telemetry::{AttributeValue, Attributes, Event, Severity, TraceContext};
use std::time::{Duration, UNIX_EPOCH};

/// Schema version of the payload body. Bump on any incompatible change; the
/// decoder rejects anything it does not recognize rather than guessing.
pub const EVENT_PAYLOAD_SCHEMA: u16 = 1;

/// `Envelope.event_type` under which telemetry events are spooled.
///
/// The spool bounds this to 256 ASCII bytes, and a forwarder dispatches on it,
/// so it is a stable identifier rather than a description.
pub const EVENT_TYPE: &str = "telemetry.event";

const SEVERITY_TRACE: u8 = 1;
const SEVERITY_DEBUG: u8 = 2;
const SEVERITY_INFO: u8 = 3;
const SEVERITY_WARN: u8 = 4;
const SEVERITY_ERROR: u8 = 5;
const SEVERITY_CRITICAL: u8 = 6;

const VALUE_STRING: u8 = 1;
const VALUE_SIGNED: u8 = 2;
const VALUE_UNSIGNED: u8 = 3;
const VALUE_FLOAT: u8 = 4;
const VALUE_BOOLEAN: u8 = 5;
const VALUE_REDACTED: u8 = 6;

/// Upper bound on one encoded payload, derived from the attribute bounds plus
/// a fixed envelope allowance. `SpoolConfig::maximum_event_bytes` defaults to
/// 4 MiB, comfortably above this, so a well-formed event is never rejected for
/// size by the spool itself.
pub const MAX_PAYLOAD_BYTES: usize = Attributes::MAX_ATTRIBUTES
    * (Attributes::MAX_KEY_LEN + Attributes::MAX_STRING_LEN + 16)
    + Event::MAX_NAME_LEN
    + 512;

/// Encodes an event as a spool payload.
pub fn encode_event(event: &Event) -> FaultResult<Vec<u8>> {
    if event.name.is_empty() || event.name.len() > Event::MAX_NAME_LEN {
        return Err(Fault::invalid_argument(
            "telemetry event name is out of bounds",
        ));
    }
    if event.event_id.kind() != "evt" {
        return Err(Fault::invalid_argument(
            "telemetry event identifier is not an event ID",
        ));
    }
    let timestamp = event
        .timestamp
        .duration_since(UNIX_EPOCH)
        .map_err(|error| {
            Fault::new(
                Code::FailedPrecondition,
                "telemetry timestamp precedes the Unix epoch",
            )
            .with_source(error)
        })?;
    let timestamp_millis = u64::try_from(timestamp.as_millis()).map_err(|_| {
        Fault::new(
            Code::OutOfRange,
            "telemetry timestamp exceeds u64 milliseconds",
        )
    })?;
    let attribute_count = u32::try_from(event.attributes.len())
        .map_err(|_| Fault::new(Code::OutOfRange, "telemetry attribute count exceeds u32"))?;

    let mut encoder = Encoder::new();
    encoder.u16(EVENT_PAYLOAD_SCHEMA);
    encoder.string(&event.event_id.to_string())?;
    encoder.string(&event.name)?;
    encoder.u8(severity_code(event.severity));
    encoder.u64(timestamp_millis);
    // An ill-formed trace context is dropped rather than persisted, matching
    // what `libs/go/observability.TraceContext.Attributes` does with one: a
    // correlation key no collector can join on is worse than none, because it
    // looks like a link that simply has no other end.
    match event.trace.as_ref().filter(|trace| trace.is_valid()) {
        Some(trace) => {
            encoder.bool(true);
            encoder.string(&trace.trace_id.to_ascii_lowercase())?;
            encoder.string(&trace.span_id.to_ascii_lowercase())?;
            encoder.bool(trace.sampled);
        }
        None => encoder.bool(false),
    }
    encoder.u32(attribute_count);
    for (key, value) in event.attributes.iter() {
        encoder.string(key)?;
        encode_value(&mut encoder, value)?;
    }
    Ok(encoder.into_bytes())
}

/// Decodes a spool payload back into an event, re-validating every bound.
pub fn decode_event(bytes: &[u8]) -> FaultResult<Event> {
    let mut decoder = Decoder::new(bytes, MAX_PAYLOAD_BYTES)?;
    if decoder.u16()? != EVENT_PAYLOAD_SCHEMA {
        return Err(Fault::new(
            Code::FailedPrecondition,
            "telemetry event payload schema is unsupported",
        ));
    }
    let event_id = decoder
        .string()?
        .parse::<mindclade_identifiers::ResourceId>()
        .map_err(|error| {
            Fault::data_loss("telemetry event identifier is invalid").with_source(error)
        })?;
    if event_id.kind() != "evt" {
        return Err(Fault::data_loss(
            "telemetry event identifier is not an event ID",
        ));
    }
    let name = decoder.string()?.to_owned();
    if name.is_empty() || name.len() > Event::MAX_NAME_LEN {
        return Err(Fault::data_loss("telemetry event name is out of bounds"));
    }
    let severity = severity_from_code(decoder.u8()?)?;
    let timestamp_millis = decoder.u64()?;
    let trace = if decoder.bool()? {
        let trace = TraceContext {
            trace_id: decoder.string()?.to_owned(),
            span_id: decoder.string()?.to_owned(),
            sampled: decoder.bool()?,
        };
        if !trace.is_valid() {
            return Err(Fault::data_loss("telemetry trace context is malformed"));
        }
        Some(trace)
    } else {
        None
    };

    let declared = decoder.u32()?;
    let count = usize::try_from(declared)
        .map_err(|_| Fault::data_loss("telemetry attribute count is invalid"))?;
    // Bound before the loop rather than trusting the loop to terminate on
    // truncation: the count is attacker-influenced the moment a segment file
    // is, and `Attributes::MAX_ATTRIBUTES` is a far tighter ceiling than the
    // decoder's generic item cap.
    if count > Attributes::MAX_ATTRIBUTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "telemetry attribute count exceeds the bound",
        ));
    }
    let mut attributes = Attributes::new();
    for _ in 0..count {
        let key = decoder.string()?.to_owned();
        let value = decode_value(&mut decoder)?;
        // `insert` re-applies every attribute bound — key length, reserved
        // names, value length, float finiteness — so a segment written by an
        // older or tampered-with producer cannot smuggle an out-of-bounds
        // attribute back into the process.
        if !attributes.insert(key, value) {
            return Err(Fault::data_loss("telemetry attribute violates its bounds"));
        }
    }
    decoder.finish()?;

    Ok(Event {
        event_id,
        name,
        severity,
        timestamp: UNIX_EPOCH + Duration::from_millis(timestamp_millis),
        trace,
        attributes,
    })
}

const fn severity_code(severity: Severity) -> u8 {
    // Exhaustive without a wildcard: a new severity must be given a stable
    // code here, not silently folded into an existing one on disk.
    match severity {
        Severity::Trace => SEVERITY_TRACE,
        Severity::Debug => SEVERITY_DEBUG,
        Severity::Info => SEVERITY_INFO,
        Severity::Warn => SEVERITY_WARN,
        Severity::Error => SEVERITY_ERROR,
        Severity::Critical => SEVERITY_CRITICAL,
    }
}

fn severity_from_code(code: u8) -> FaultResult<Severity> {
    match code {
        SEVERITY_TRACE => Ok(Severity::Trace),
        SEVERITY_DEBUG => Ok(Severity::Debug),
        SEVERITY_INFO => Ok(Severity::Info),
        SEVERITY_WARN => Ok(Severity::Warn),
        SEVERITY_ERROR => Ok(Severity::Error),
        SEVERITY_CRITICAL => Ok(Severity::Critical),
        _ => Err(Fault::data_loss("telemetry severity code is unknown")),
    }
}

fn encode_value(encoder: &mut Encoder, value: &AttributeValue) -> FaultResult<()> {
    match value {
        AttributeValue::String(text) => {
            encoder.u8(VALUE_STRING);
            encoder.string(text)?;
        }
        AttributeValue::Signed(number) => {
            encoder.u8(VALUE_SIGNED);
            // Two's complement through u64 keeps the encoder on `record_io`'s
            // fixed-width big-endian primitives; `as` here is a bit
            // reinterpretation, exactly inverted on decode.
            encoder.u64(number.cast_unsigned());
        }
        AttributeValue::Unsigned(number) => {
            encoder.u8(VALUE_UNSIGNED);
            encoder.u64(*number);
        }
        AttributeValue::Float(number) => {
            encoder.u8(VALUE_FLOAT);
            encoder.u64(number.to_bits());
        }
        AttributeValue::Boolean(flag) => {
            encoder.u8(VALUE_BOOLEAN);
            encoder.bool(*flag);
        }
        AttributeValue::Redacted => encoder.u8(VALUE_REDACTED),
        // `AttributeValue` is `#[non_exhaustive]` and this crate is not its
        // home, so a variant added upstream lands here at runtime rather than
        // at compile time. Refuse it: persisting an attribute this codec
        // cannot name would produce a segment its own decoder rejects.
        _ => {
            return Err(Fault::invalid_argument(
                "telemetry attribute value kind is not encodable",
            ));
        }
    }
    Ok(())
}

fn decode_value(decoder: &mut Decoder<'_>) -> FaultResult<AttributeValue> {
    match decoder.u8()? {
        VALUE_STRING => Ok(AttributeValue::String(decoder.string()?.to_owned())),
        VALUE_SIGNED => Ok(AttributeValue::Signed(decoder.u64()?.cast_signed())),
        VALUE_UNSIGNED => Ok(AttributeValue::Unsigned(decoder.u64()?)),
        VALUE_FLOAT => {
            let number = f64::from_bits(decoder.u64()?);
            if !number.is_finite() {
                return Err(Fault::data_loss("telemetry float attribute is not finite"));
            }
            Ok(AttributeValue::Float(number))
        }
        VALUE_BOOLEAN => Ok(AttributeValue::Boolean(decoder.bool()?)),
        VALUE_REDACTED => Ok(AttributeValue::Redacted),
        _ => Err(Fault::data_loss("telemetry attribute value tag is unknown")),
    }
}
