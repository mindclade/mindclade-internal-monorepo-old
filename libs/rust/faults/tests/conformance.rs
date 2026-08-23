// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Runtime half of the cross-language fault contract.
//!
//! `tests/integration/cross_language/test_error_codes.py` pins that this crate,
//! `libs/go/faults`, and `mindclade.common.v1.ErrorCode` declare one code set.
//! These tests pin what that set has to *do*: accept what the Go control plane
//! actually emits, and carry retry guidance without losing the distinction
//! between an explicit refusal and silence.

use mindclade_faults::{Code, Fault, RetryHint, WireContext, WireFault, WireRetryKind};
use std::time::Duration;

fn wire(code: &str, kind: WireRetryKind, after_millis: Option<u64>) -> WireFault {
    WireFault {
        code: code.to_owned(),
        message: "boundary crossing".to_owned(),
        context: Vec::new(),
        retry_kind: kind,
        retry_after_millis: after_millis,
    }
}

#[test]
fn sensitive_context_never_crosses_wire() {
    let fault = Fault::new(Code::Internal, "x").with_sensitive_context("secret");
    let wire = WireFault::from(&fault);
    assert_eq!(wire.context[0].value, "[REDACTED]");
}

#[test]
fn go_emitted_spellings_parse() {
    // The exact strings `libs/go/faults` puts on `Mindclade-Error-Code`.
    for (emitted, expected) in [
        ("canceled", Code::Cancelled),
        ("not_implemented", Code::Unimplemented),
        ("unknown", Code::Unknown),
    ] {
        assert_eq!(
            emitted.parse::<Code>(),
            Ok(expected),
            "{emitted} did not parse"
        );
        assert_eq!(Code::from_wire(emitted), expected);
    }
}

#[test]
fn legacy_spellings_still_parse() {
    // Values carrying these are already in logs and in flight.
    assert_eq!("cancelled".parse::<Code>(), Ok(Code::Cancelled));
    assert_eq!("unimplemented".parse::<Code>(), Ok(Code::Unimplemented));
}

#[test]
fn canonical_spellings_are_what_this_crate_emits() {
    assert_eq!(Code::Cancelled.as_str(), "canceled");
    assert_eq!(Code::Unimplemented.as_str(), "not_implemented");
    assert_eq!(Code::Unknown.as_str(), "unknown");
}

#[test]
fn every_code_round_trips_through_its_wire_spelling() {
    for code in [
        Code::Unknown,
        Code::InvalidArgument,
        Code::NotFound,
        Code::AlreadyExists,
        Code::FailedPrecondition,
        Code::Aborted,
        Code::OutOfRange,
        Code::Unimplemented,
        Code::Internal,
        Code::Unavailable,
        Code::DataLoss,
        Code::Unauthenticated,
        Code::PermissionDenied,
        Code::ResourceExhausted,
        Code::DeadlineExceeded,
        Code::Cancelled,
        Code::Conflict,
    ] {
        assert_eq!(
            Code::from_wire(code.as_str()),
            code,
            "{code} did not round-trip"
        );
    }
}

#[test]
fn unrecognized_peer_code_degrades_instead_of_failing() {
    // A peer on a newer build sends a code this one has never heard of.
    assert_eq!(Code::from_wire("teapot"), Code::Unknown);
    // Strict parsing stays strict, so a typo in configuration still fails loudly.
    assert!("teapot".parse::<Code>().is_err());
    // Parsing is bounded: an over-long peer value is rejected on length.
    assert!("x".repeat(4096).parse::<Code>().is_err());
    assert_eq!(Code::from_wire(&"x".repeat(4096)), Code::Unknown);
}

#[test]
fn explicit_refusal_is_distinguishable_from_silence() {
    // Both previously serialized as an absent delay, so a client could not tell
    // "do not retry" from "no guidance" and fell back to its own policy for both.
    let refused = Fault::new(Code::Unavailable, "shed").with_retry_hint(RetryHint::Never);
    let projection = WireFault::from(&refused);
    assert_eq!(projection.retry_kind, WireRetryKind::Never);
    assert_eq!(projection.retry_after_millis, None);
    assert_eq!(projection.to_fault().retry_hint(), RetryHint::Never);

    // Silence on a transient code falls back to the local default for that code.
    let silent = wire("unavailable", WireRetryKind::Unspecified, None);
    assert_eq!(silent.to_fault().retry_hint(), RetryHint::Immediate);
}

#[test]
fn backoff_is_readable_on_ingestion() {
    let hinted = wire("unavailable", WireRetryKind::WithBackoff, None);
    // A peer's with_backoff is now expressible and readable at all; before, the
    // kind had no wire representation. The in-memory hint is Immediate, which in
    // this crate already means "retry subject to the caller's own schedule".
    assert_eq!(
        WireRetryKind::from_wire("with_backoff").as_str(),
        "with_backoff"
    );
    assert_eq!(hinted.to_fault().retry_hint(), RetryHint::Immediate);
}

#[test]
fn reprojecting_a_relayed_fault_narrows_the_kind() {
    // Pins the documented emit-side limitation rather than hiding it: RetryHint
    // has no Backoff state, so a relay that ingests with_backoff and re-projects
    // the resulting Fault emits immediate. A relay must forward the WireFault
    // itself. This assertion is expected to change when RetryHint gains
    // Unspecified/Backoff -- see the impl doc on `From<&Fault> for WireFault`.
    let relayed =
        WireFault::from(&wire("unavailable", WireRetryKind::WithBackoff, None).to_fault());
    assert_eq!(relayed.retry_kind, WireRetryKind::Immediate);
}

#[test]
fn delayed_retry_round_trips_and_normalizes() {
    let delayed = Fault::new(Code::ResourceExhausted, "slow down")
        .with_retry_hint(RetryHint::After(Duration::from_millis(2500)));
    let projection = WireFault::from(&delayed);
    assert_eq!(projection.retry_kind, WireRetryKind::After);
    assert_eq!(projection.retry_after_millis, Some(2500));
    assert_eq!(
        projection.to_fault().retry_hint(),
        RetryHint::After(Duration::from_millis(2500))
    );

    // Mirrors Go's RetryPolicy.Normalized: a non-positive delay is an immediate retry.
    let zero = wire("unavailable", WireRetryKind::After, Some(0));
    assert_eq!(zero.to_fault().retry_hint(), RetryHint::Immediate);
    let missing = wire("unavailable", WireRetryKind::After, None);
    assert_eq!(missing.to_fault().retry_hint(), RetryHint::Immediate);
}

#[test]
fn unreadable_retry_kind_leaves_the_caller_on_its_own_policy() {
    assert_eq!(
        WireRetryKind::from_wire("no_such_kind"),
        WireRetryKind::Unspecified
    );
    assert_eq!(WireRetryKind::from_wire(""), WireRetryKind::Unspecified);
    assert_eq!(
        WireRetryKind::from_wire("with_backoff"),
        WireRetryKind::WithBackoff
    );
}

#[test]
fn redacted_context_returns_as_sensitive_not_as_text() {
    let outbound = Fault::new(Code::Unauthenticated, "token rejected")
        .with_sensitive_context("token")
        .with_context("tenant", "tenant-1");
    let restored = WireFault::from(&outbound).to_fault();
    assert!(restored.to_string().contains("token=[REDACTED]"));
    assert!(restored.to_string().contains("tenant=tenant-1"));
}

#[test]
fn over_long_context_keys_are_dropped_not_merged() {
    // Truncating keys would collapse these two distinct entries into one and
    // silently discard a value, because Context is a map.
    let mut projection = wire("internal", WireRetryKind::Never, None);
    let shared = "k".repeat(200);
    projection.context = vec![
        WireContext {
            key: format!("{shared}-alpha"),
            value: "first".to_owned(),
        },
        WireContext {
            key: format!("{shared}-beta"),
            value: "second".to_owned(),
        },
        WireContext {
            key: "kept".to_owned(),
            value: "third".to_owned(),
        },
    ];

    let fault = projection.to_fault();
    assert_eq!(fault.context().iter().count(), 1);
    assert!(fault.context().get("kept").is_some());
}

#[test]
fn inbound_context_is_bounded() {
    let mut projection = wire("internal", WireRetryKind::Never, None);
    projection.context = (0..4096)
        .map(|index| WireContext {
            key: format!("key-{index}"),
            value: "v".repeat(8192),
        })
        .collect();
    projection.message = "m".repeat(65536);

    let fault = projection.to_fault();
    assert_eq!(fault.context().iter().count(), 64);
    assert!(fault.message().len() <= 2048);
    for (_, value) in fault.context().iter() {
        assert!(value.to_string().len() <= 512);
    }
}
