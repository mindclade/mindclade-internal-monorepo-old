// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_runtime_core::{Deadline, ManualClock};
use std::time::{Duration, Instant, SystemTime};

#[test]
fn deadline_is_monotonic_and_expiry_is_zero_remaining() {
    let clock = ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now());
    let deadline = Deadline::after(&clock, Duration::from_millis(10)).expect("deadline");
    assert!(!deadline.is_expired(&clock));
    clock.advance(Duration::from_millis(10));
    assert!(deadline.is_expired(&clock));
    assert_eq!(deadline.remaining(&clock), Duration::ZERO);
}
