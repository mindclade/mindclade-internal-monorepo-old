// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Adversarial regressions for the primitives themselves.
//!
//! These assert the properties the rest of the repository relies on: the budget
//! aborts, the ceilings are inclusive exactly where documented, and the recovery
//! diagnostic sink cannot be grown without bound by hostile input.

use mindclade_bounded_parse::{
    AllocationBudget, Cursor, Diagnostic, Limits, Location, MAXIMUM_MESSAGE_BYTES, ParseMode,
    Recovery, Source,
};
use mindclade_bytes_io::ByteSize;

fn diagnostic(message: &str) -> Diagnostic {
    Diagnostic {
        location: Location::start(),
        code: "test.diagnostic",
        message: message.to_owned(),
    }
}

// ---------------------------------------------------------------------------
// Recovery diagnostics
// ---------------------------------------------------------------------------

#[test]
fn recovery_diagnostics_do_not_grow_without_bound() {
    // Recovery mode records one diagnostic per malformed construct. A hostile
    // input is *all* malformed constructs, so an uncapped sink turns a bounded
    // parse into an unbounded retained allocation.
    let mut recovery =
        Recovery::new(ParseMode::Recovery, Limits::default()).expect("limits are valid");
    for index in 0..100_000_u32 {
        let _ = recovery.record(diagnostic(&format!("malformed construct {index}")));
    }
    assert!(
        recovery.diagnostics().len() <= Limits::default().maximum_metadata_entries,
        "recovery retained {} diagnostics, above the configured ceiling",
        recovery.diagnostics().len()
    );
}

#[test]
fn recovery_truncates_at_the_ceiling_and_counts_what_it_dropped() {
    // Truncation, not failure: aborting the parse on diagnostic 5 would make
    // recovery mode strict mode with extra steps. The loss must be visible.
    let limits = Limits {
        maximum_metadata_entries: 4,
        ..Limits::default()
    };
    let mut recovery = Recovery::new(ParseMode::Recovery, limits).expect("limits are valid");
    assert_eq!(recovery.maximum_diagnostics(), 4);
    for index in 0..1_000_u32 {
        recovery
            .record(diagnostic(&format!("defect {index}")))
            .expect("recovery keeps recovering past the ceiling");
    }
    assert_eq!(recovery.diagnostics().len(), 4);
    assert_eq!(recovery.suppressed(), 996);
}

#[test]
fn an_oversized_diagnostic_message_is_clamped_rather_than_aborting_the_parse() {
    // A recovery-mode reporter quotes the construct it rejected, and a hostile
    // line may run to `maximum_line_bytes`. Returning an error here would abort
    // through the parser's `?` and turn recovery into strict mode under exactly
    // the input recovery exists to survive.
    let mut recovery =
        Recovery::new(ParseMode::Recovery, Limits::default()).expect("limits are valid");
    recovery
        .record(diagnostic(&"x".repeat(MAXIMUM_MESSAGE_BYTES * 4)))
        .expect("an oversized message is clamped, not rejected");
    assert_eq!(recovery.diagnostics().len(), 1);
    assert_eq!(
        recovery.diagnostics()[0].message.len(),
        MAXIMUM_MESSAGE_BYTES
    );
}

#[test]
fn clamping_a_message_never_splits_a_character() {
    // The quoted construct is arbitrary UTF-8 from the input, and a 3-byte
    // character does not divide the bound evenly, so the clamp has to walk back
    // to a boundary or `String::truncate` panics.
    let mut recovery =
        Recovery::new(ParseMode::Recovery, Limits::default()).expect("limits are valid");
    recovery
        .record(diagnostic(&"\u{4e00}".repeat(MAXIMUM_MESSAGE_BYTES)))
        .expect("multi-byte messages are clamped safely");
    let clamped = &recovery.diagnostics()[0].message;
    assert!(clamped.len() <= MAXIMUM_MESSAGE_BYTES);
    // 4096 is not a multiple of 3, so a correct clamp lands just below it.
    assert_eq!(clamped.len(), MAXIMUM_MESSAGE_BYTES - 1);
}

#[test]
fn a_diagnostic_with_an_out_of_bounds_code_is_still_an_error() {
    // The code is a constant the parser author picked, not input-derived, so an
    // invalid one stays a bug worth surfacing rather than data to clamp.
    let mut recovery =
        Recovery::new(ParseMode::Recovery, Limits::default()).expect("limits are valid");
    recovery
        .record(Diagnostic {
            location: Location::start(),
            code: "",
            message: "empty code".to_owned(),
        })
        .expect_err("an empty diagnostic code is a parser bug");
    assert!(recovery.diagnostics().is_empty());
}

#[test]
fn recovery_refuses_unvalidated_limits() {
    let zeroed = Limits {
        maximum_metadata_entries: 0,
        ..Limits::default()
    };
    Recovery::new(ParseMode::Recovery, zeroed).expect_err("a zero ceiling is not a ceiling");
}

#[test]
fn strict_mode_retains_nothing_and_still_succeeds() {
    let mut recovery =
        Recovery::new(ParseMode::Strict, Limits::default()).expect("limits are valid");
    for index in 0..10_000_u32 {
        recovery
            .record(diagnostic(&format!("defect {index}")))
            .expect("strict mode discards rather than fails");
    }
    // Read the counter off the sink that actually absorbed those 10,000
    // diagnostics, before `into_diagnostics` consumes it. Asserting on a freshly
    // built `Recovery` instead would pass even if strict mode started counting
    // suppressions — which is the regression this line exists to pin.
    assert_eq!(
        recovery.suppressed(),
        0,
        "strict mode discards; it does not suppress"
    );
    assert!(recovery.into_diagnostics().is_empty());
}

// ---------------------------------------------------------------------------
// Allocation budget
// ---------------------------------------------------------------------------

#[test]
fn allocation_budget_aborts_at_the_ceiling_and_does_not_retain_rejected_charges() {
    let limits = Limits {
        maximum_input_bytes: ByteSize::new(16),
        maximum_payload_bytes: ByteSize::new(16),
        ..Limits::default()
    };
    let mut budget = AllocationBudget::from_limits(limits);
    budget.charge(16).expect("the ceiling itself is chargeable");
    assert_eq!(budget.used(), 16);
    assert_eq!(budget.remaining().expect("remaining"), 0);
    budget
        .charge(1)
        .expect_err("a charge past the ceiling must fail");
    assert_eq!(budget.used(), 16, "a rejected charge must not be retained");
}

#[test]
fn allocation_budget_rejects_accounting_overflow() {
    let mut budget = AllocationBudget::from_limits(Limits::default());
    budget
        .charge(u64::MAX)
        .expect_err("a charge above the ceiling must fail");
    budget.charge(1).expect("accounting is still consistent");
    budget
        .charge(u64::MAX)
        .expect_err("overflowing the accumulator must fail, not wrap");
}

// ---------------------------------------------------------------------------
// Ceiling inclusivity, exactly as documented on `Limits`
// ---------------------------------------------------------------------------

#[test]
fn input_ceiling_is_inclusive() {
    let limits = Limits {
        maximum_input_bytes: ByteSize::new(4),
        maximum_payload_bytes: ByteSize::new(4),
        ..Limits::default()
    };
    Source::new(b"abcd", limits).expect("an input of exactly the ceiling is accepted");
    Source::new(b"abcde", limits).expect_err("one byte past the ceiling is rejected");
}

#[test]
fn line_ceiling_is_inclusive_and_counts_the_carriage_return() {
    let limits = Limits {
        maximum_line_bytes: 4,
        ..Limits::default()
    };
    let mut exact = Cursor::new(b"abcd\n", limits).expect("cursor");
    assert_eq!(exact.next_line().expect("line").expect("some").1, b"abcd");

    let mut over = Cursor::new(b"abcde\n", limits).expect("cursor");
    over.next_line().expect_err("one byte past the ceiling");

    // `\r` is part of the line for accounting even though it is stripped from
    // the yielded slice; a hostile input cannot buy a byte by using CRLF.
    let mut crlf = Cursor::new(b"abcd\r\n", limits).expect("cursor");
    crlf.next_line()
        .expect_err("CRLF must not smuggle a byte past the line ceiling");
}

#[test]
fn cursor_strips_carriage_returns_without_underflowing() {
    let mut cursor = Cursor::new(b"\r\n\r\nabc\r\n", Limits::default()).expect("cursor");
    assert_eq!(cursor.next_line().expect("line").expect("some").1, b"");
    assert_eq!(cursor.next_line().expect("line").expect("some").1, b"");
    assert_eq!(cursor.next_line().expect("line").expect("some").1, b"abc");
    assert!(cursor.next_line().expect("line").is_none());
}

#[test]
fn zero_and_inconsistent_limits_are_rejected() {
    let zeroed = Limits {
        maximum_records: 0,
        ..Limits::default()
    };
    zeroed.validate().expect_err("zero ceilings are rejected");

    let inconsistent = Limits {
        maximum_input_bytes: ByteSize::new(1024),
        maximum_payload_bytes: ByteSize::new(1),
        ..Limits::default()
    };
    inconsistent
        .validate()
        .expect_err("input far above the allocation ceiling is rejected");

    // `Cursor` must refuse unvalidated limits rather than silently parse with them.
    Cursor::new(b"abc", zeroed).expect_err("cursor validates its limits");
}
