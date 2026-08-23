// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Transport-neutral projection of a [`Fault`], and the total path back.
//!
//! The shape mirrors `mindclade.common.v1.ErrorDetail`; the retry kind strings
//! mirror `faults.RetryKind` in `libs/go/faults/retry.go`.
//! `tests/integration/cross_language/test_error_codes.py` fails if they drift.

use crate::{Code, ContextValue, Fault, RetryHint};
use core::time::Duration;

/// Redaction marker written in place of a sensitive context value.
const REDACTED: &str = "[REDACTED]";

/// An inbound fault is peer-controlled, so every parse it feeds is bounded.
/// Entries past the cap are dropped and over-long strings are truncated at a
/// UTF-8 boundary; a diagnostic projection is never worth an unbounded
/// allocation driven by a remote sender.
const MAXIMUM_CONTEXT_ENTRIES: usize = 64;
const MAXIMUM_MESSAGE_BYTES: usize = 2048;
const MAXIMUM_CONTEXT_KEY_BYTES: usize = 128;
const MAXIMUM_CONTEXT_VALUE_BYTES: usize = 512;

/// How a caller may retry, as it appears on the wire.
///
/// Separate from [`RetryHint`] because the wire has to carry a distinction the
/// in-memory hint does not model: `Never` is an explicit refusal, `Unspecified`
/// is silence, and previously both serialized as an absent
/// `retry_after_millis`. `WithBackoff` exists so the Go control plane's
/// `with_backoff` survives the crossing instead of collapsing onto `Immediate`
/// and losing the sender's intent that the caller pace itself.
// `#[non_exhaustive]` for the same reason `Code` carries it: this enum mirrors a
// proto enum that will gain values, and without the attribute the next
// `RetryKind` added to `errors.proto` is a source-breaking change for every
// crate that matches on this one.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
#[non_exhaustive]
pub enum WireRetryKind {
    /// No guidance. The receiver falls back to its own policy for the code.
    #[default]
    Unspecified,
    /// An explicit refusal to retry.
    Never,
    Immediate,
    /// Retry under the caller's own backoff algorithm.
    WithBackoff,
    /// Retry no earlier than `retry_after_millis`.
    After,
}

impl WireRetryKind {
    /// Returns the stable wire representation. Silence serializes as empty,
    /// matching `faults.RetryKindUnspecified` on the Go side.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Unspecified => "",
            Self::Never => "never",
            Self::Immediate => "immediate",
            Self::WithBackoff => "with_backoff",
            Self::After => "after",
        }
    }
    /// Parses a wire retry kind, degrading anything unrecognized to
    /// [`WireRetryKind::Unspecified`].
    ///
    /// Total on purpose: an unreadable retry hint must leave the caller on its
    /// own policy, never reject the fault that carried it.
    #[must_use]
    pub fn from_wire(value: &str) -> Self {
        match value.trim() {
            "never" => Self::Never,
            "immediate" => Self::Immediate,
            "with_backoff" => Self::WithBackoff,
            "after" => Self::After,
            _ => Self::Unspecified,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WireContext {
    pub key: String,
    pub value: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WireFault {
    pub code: String,
    pub message: String,
    pub context: Vec<WireContext>,
    pub retry_kind: WireRetryKind,
    /// Set only when `retry_kind` is [`WireRetryKind::After`]. A delay on any
    /// other kind is ignored on ingestion rather than silently overriding it.
    pub retry_after_millis: Option<u64>,
}

/// Projects the *local* hint, which is why the emit side is narrower than the
/// wire vocabulary.
///
/// [`RetryHint`] has three states, so this direction can only ever produce
/// `after`, `immediate`, or `never` — never `unspecified` or `with_backoff`.
/// Two consequences worth knowing before relying on it:
///
///  * A relay that ingests a peer's `with_backoff` via [`WireFault::to_fault`]
///    and re-projects the result emits `immediate`. Forward the [`WireFault`]
///    itself across a relay; `to_fault` is for terminal consumption.
///  * [`Fault::new`] derives `Never` from any non-transient code without the
///    author deciding anything, so a default-constructed fault now asserts
///    `never` on the wire rather than staying silent. That is faithful to what
///    the fault says locally, and it is the distinction this field exists to
///    carry — but it is an inference, not an authored refusal.
///
/// Closing both needs `RetryHint::{Unspecified, Backoff}`, which is a
/// source-breaking change for the exhaustive match in `libs/rust/data_stream`
/// and so is deliberately not made here.
impl From<&Fault> for WireFault {
    fn from(fault: &Fault) -> Self {
        let context = fault
            .context()
            .iter()
            .map(|(key, value)| WireContext {
                key: key.to_owned(),
                value: match value {
                    ContextValue::Sensitive => REDACTED.to_owned(),
                    _ => value.to_string(),
                },
            })
            .collect();
        let (retry_kind, retry_after_millis) = match fault.retry_hint() {
            RetryHint::After(duration) => match u64::try_from(duration.as_millis()) {
                Ok(millis) => (WireRetryKind::After, Some(millis)),
                // A delay larger than the wire representation is reported as
                // non-retryable rather than silently clamped to an unrelated
                // delay. Emitting the refusal explicitly is what keeps it
                // distinguishable from having said nothing at all.
                Err(_) => (WireRetryKind::Never, None),
            },
            RetryHint::Immediate => (WireRetryKind::Immediate, None),
            RetryHint::Never => (WireRetryKind::Never, None),
        };
        Self {
            code: fault.code().as_str().to_owned(),
            message: fault.message().to_owned(),
            context,
            retry_kind,
            retry_after_millis,
        }
    }
}

impl WireFault {
    /// Reconstructs a fault from a peer's projection.
    ///
    /// Total: an unrecognized code becomes [`Code::Unknown`] and unreadable
    /// retry guidance becomes the local default for that code, so a peer running
    /// a newer build can never make this one fail to read a fault.
    ///
    /// Deliberately an inherent method rather than `impl From<&WireFault> for
    /// Fault`. `Fault` is the workspace-wide error type, so a second `From` impl
    /// for it makes `?` inside a closure ambiguous and breaks type inference in
    /// every crate that relies on the blanket `From<T> for T` — it did exactly
    /// that in `services/runtime_host` when this was written as a `From`.
    #[must_use]
    pub fn to_fault(&self) -> Fault {
        let code = Code::from_wire(&self.code);
        let mut fault = Fault::new(code, truncate(&self.message, MAXIMUM_MESSAGE_BYTES))
            .with_retry_hint(retry_hint(code, self.retry_kind, self.retry_after_millis));
        for entry in self.context.iter().take(MAXIMUM_CONTEXT_ENTRIES) {
            // An over-long key is dropped rather than truncated. `Context` is a
            // map, so two keys sharing a truncated prefix would collapse into
            // one entry and silently discard the other value; losing one
            // absurdly-named field is the honest outcome.
            if entry.key.len() > MAXIMUM_CONTEXT_KEY_BYTES {
                continue;
            }
            // A value the sender redacted stays redacted: it is restored as
            // Sensitive, not as a string that happens to read "[REDACTED]".
            fault = if entry.value == REDACTED {
                fault.with_sensitive_context(entry.key.clone())
            } else {
                fault.with_context(
                    entry.key.clone(),
                    truncate(&entry.value, MAXIMUM_CONTEXT_VALUE_BYTES),
                )
            };
        }
        fault
    }
}

/// Maps wire retry guidance onto the in-memory hint.
///
/// `WithBackoff` lands on `Immediate` because this crate's `Immediate` already
/// means "retry now, subject to the caller's own backoff schedule" — see the
/// retry loop in `libs/rust/data_stream`, which feeds `Immediate` through its
/// own `retry_policy.delay(attempt, seed)`. The distinction stays intact on the
/// wire, which is where a client reads it.
fn retry_hint(code: Code, kind: WireRetryKind, after_millis: Option<u64>) -> RetryHint {
    match kind {
        WireRetryKind::Unspecified => crate::retry::default_retry_hint(code),
        WireRetryKind::Never => RetryHint::Never,
        WireRetryKind::Immediate | WireRetryKind::WithBackoff => RetryHint::Immediate,
        // Mirrors `RetryPolicy.Normalized` on the Go side, which rewrites a
        // non-positive delay to an immediate retry rather than a zero wait.
        WireRetryKind::After => match after_millis {
            Some(millis) if millis > 0 => RetryHint::After(Duration::from_millis(millis)),
            _ => RetryHint::Immediate,
        },
    }
}

/// Truncates at a UTF-8 boundary at or below `limit` bytes.
fn truncate(value: &str, limit: usize) -> String {
    if value.len() <= limit {
        return value.to_owned();
    }
    let mut end = limit;
    while end > 0 && !value.is_char_boundary(end) {
        end -= 1;
    }
    value[..end].to_owned()
}
