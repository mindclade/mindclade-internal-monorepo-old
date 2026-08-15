// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Code, Fault, RetryHint};
use std::time::Duration;

#[test]
fn sensitive_context_is_not_retained_or_displayed() {
    let fault = Fault::new(Code::Unauthenticated, "token rejected")
        .with_sensitive_context("token")
        .with_context("tenant", "tenant-1");
    let rendered = fault.to_string();
    assert!(rendered.contains("token=[REDACTED]"));
    assert!(rendered.contains("tenant=tenant-1"));
}

#[test]
fn retry_hint_can_be_explicit() {
    let fault = Fault::new(Code::Unavailable, "backend unavailable")
        .with_retry_hint(RetryHint::After(Duration::from_secs(2)));
    assert_eq!(fault.retry_hint(), RetryHint::After(Duration::from_secs(2)));
}

#[test]
fn codes_round_trip() {
    let parsed = "failed_precondition".parse::<Code>();
    assert_eq!(parsed, Ok(Code::FailedPrecondition));
    assert!("made_up".parse::<Code>().is_err());
}
