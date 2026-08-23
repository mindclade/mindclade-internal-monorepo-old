// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic newline-delimited JSON rendering of an [`Event`].
//!
//! Encoding only, on purpose. A decoder would be a parser over whatever a log
//! file happens to contain — an untrusted-input surface this crate has no
//! reason to own. Records that must survive a round trip inside the fleet go
//! through the spool's binary envelope instead, which is length-delimited and
//! bounded by construction.
//!
//! # Shape
//!
//! One object per line, members in a fixed order:
//!
//! ```text
//! {"time":"2026-08-23T00:00:00.000Z","level":"INFO","msg":"checkpoint.committed",
//!  "event.id":"evt_0193...","trace.id":"...","span.id":"...","trace.sampled":true,
//!  "step":42,"token":"[REDACTED]"}
//! ```
//!
//! `time`/`level`/`msg` are the member names `log/slog`'s JSON handler emits on
//! the Go side, and `trace.id`/`span.id`/`trace.sampled` are exactly the keys
//! `libs/go/observability.TraceContext.Attributes` produces. Attributes are
//! flattened as siblings, as `slog` flattens its attrs, so one collector
//! pipeline parses both tiers without a per-language schema.
//! [`Attributes::RESERVED_KEYS`] keeps an attribute from colliding with an
//! envelope member.

use crate::{AttributeValue, Attributes, Event};
use mindclade_faults::{Code, Fault, FaultResult};
use std::fmt::Write as _;
use std::time::{SystemTime, UNIX_EPOCH};

/// Upper bound on one encoded record, derived from the attribute bounds:
/// 64 attributes of a 64-byte key and a 2048-byte value, plus the envelope and
/// worst-case `\u00XX` escaping of every byte. A staging buffer at least this
/// large always admits one maximal record.
pub const MAX_RECORD_BYTES: usize = 6
    * (Attributes::MAX_ATTRIBUTES * (Attributes::MAX_KEY_LEN + Attributes::MAX_STRING_LEN + 8))
    + 1024;

/// Encodes one event as a single JSON object, without a trailing newline.
pub fn encode_event(event: &Event) -> FaultResult<String> {
    if event.name.is_empty() || event.name.len() > Event::MAX_NAME_LEN {
        return Err(Fault::invalid_argument(
            "telemetry event name is out of bounds",
        ));
    }
    let mut line = String::with_capacity(256);
    line.push('{');
    write_member(&mut line, "time", true);
    write_string(&mut line, &format_rfc3339_millis(event.timestamp)?);
    write_member(&mut line, "level", false);
    write_string(&mut line, event.severity.as_str());
    write_member(&mut line, "msg", false);
    write_string(&mut line, &event.name);
    write_member(&mut line, "event.id", false);
    write_string(&mut line, &event.event_id.to_string());
    if let Some(trace) = event.trace.as_ref().filter(|trace| trace.is_valid()) {
        write_member(&mut line, "trace.id", false);
        write_string(&mut line, &trace.trace_id.to_ascii_lowercase());
        write_member(&mut line, "span.id", false);
        write_string(&mut line, &trace.span_id.to_ascii_lowercase());
        write_member(&mut line, "trace.sampled", false);
        line.push_str(if trace.sampled { "true" } else { "false" });
    }
    for (key, value) in event.attributes.iter() {
        write_member(&mut line, key, false);
        write_value(&mut line, value);
    }
    line.push('}');
    Ok(line)
}

fn write_member(output: &mut String, key: &str, first: bool) {
    if !first {
        output.push(',');
    }
    write_string(output, key);
    output.push(':');
}

/// Renders one attribute value.
///
/// Matched exhaustively, without a wildcard arm, on purpose. `AttributeValue`
/// is `#[non_exhaustive]` to outside crates, but inside its own crate a new
/// variant makes this match fail to compile — which is what we want. A
/// wildcard would instead let a future variant reach production log records as
/// whatever placeholder the wildcard happened to pick.
fn write_value(output: &mut String, value: &AttributeValue) {
    match value {
        AttributeValue::String(text) => write_string(output, text),
        AttributeValue::Signed(number) => {
            let _ = write!(output, "{number}");
        }
        AttributeValue::Unsigned(number) => {
            let _ = write!(output, "{number}");
        }
        // `{:?}` is the shortest representation that round-trips through
        // `f64::from_str`, and always renders a fraction or an exponent so the
        // token stays a JSON number. `Attributes::insert` has already rejected
        // NaN and the infinities, which JSON cannot spell.
        AttributeValue::Float(number) => {
            let _ = write!(output, "{number:?}");
        }
        AttributeValue::Boolean(flag) => output.push_str(if *flag { "true" } else { "false" }),
        AttributeValue::Redacted => write_string(output, crate::REDACTED_TEXT),
    }
}

/// Writes a JSON string literal, escaping per RFC 8259 section 7.
///
/// Control characters are the load-bearing case: a raw newline inside an
/// attribute value would split one record into two lines, and a newline-
/// delimited reader would treat the tail as a separate — and malformed —
/// record. That is log injection, reachable from any attribute whose value
/// came from outside the process.
fn write_string(output: &mut String, value: &str) {
    output.push('"');
    for character in value.chars() {
        match character {
            '"' => output.push_str("\\\""),
            '\\' => output.push_str("\\\\"),
            '\n' => output.push_str("\\n"),
            '\r' => output.push_str("\\r"),
            '\t' => output.push_str("\\t"),
            '\u{08}' => output.push_str("\\b"),
            '\u{0c}' => output.push_str("\\f"),
            control if control < '\u{20}' || control == '\u{7f}' => {
                let _ = write!(output, "\\u{:04x}", u32::from(control));
            }
            other => output.push(other),
        }
    }
    output.push('"');
}

/// Formats a wall-clock instant as RFC 3339 UTC with fixed millisecond
/// precision.
///
/// Fixed width rather than `slog`'s trailing-zero-trimming RFC3339Nano so that
/// two renderings of one instant are byte-identical, which is what makes the
/// encoder testable against an expected line. Millisecond resolution is also
/// the resolution the durable spool envelope stores, so the two paths cannot
/// disagree about an event's timestamp.
pub fn format_rfc3339_millis(time: SystemTime) -> FaultResult<String> {
    let elapsed = time.duration_since(UNIX_EPOCH).map_err(|error| {
        Fault::new(
            Code::FailedPrecondition,
            "telemetry timestamp precedes the Unix epoch",
        )
        .with_source(error)
    })?;
    let seconds = i64::try_from(elapsed.as_secs())
        .map_err(|_| Fault::new(Code::OutOfRange, "telemetry timestamp exceeds i64 seconds"))?;
    let millis = elapsed.subsec_millis();
    let (year, month, day) = civil_from_days(seconds.div_euclid(86_400));
    let time_of_day = seconds.rem_euclid(86_400);
    let hour = time_of_day / 3_600;
    let minute = (time_of_day % 3_600) / 60;
    let second = time_of_day % 60;
    Ok(format!(
        "{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:{second:02}.{millis:03}Z"
    ))
}

/// Converts days since 1970-01-01 to a proleptic Gregorian civil date.
///
/// Hinnant's `civil_from_days`, shifted onto a 400-year era beginning
/// 0000-03-01 so that the leap day lands at the end of an era and no month
/// table is needed. Reimplemented rather than pulled in because a date
/// conversion is not worth a dependency in a foundation crate: twenty lines
/// with no allocation, no locale, and no time zone.
fn civil_from_days(days: i64) -> (i64, i64, i64) {
    let shifted = days + 719_468;
    let era = shifted.div_euclid(146_097);
    let day_of_era = shifted - era * 146_097; // [0, 146096]
    let year_of_era =
        (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365; // [0, 399]
    let year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100); // [0, 365]
    let shifted_month = (5 * day_of_year + 2) / 153; // [0, 11], March is 0
    let day = day_of_year - (153 * shifted_month + 2) / 5 + 1; // [1, 31]
    let month = if shifted_month < 10 {
        shifted_month + 3
    } else {
        shifted_month - 9
    }; // [1, 12]
    (if month <= 2 { year + 1 } else { year }, month, day)
}
