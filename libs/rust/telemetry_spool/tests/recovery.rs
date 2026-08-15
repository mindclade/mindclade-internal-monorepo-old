// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_telemetry_spool::journal::JournalPosition;

#[test]
fn pending_is_exact_and_inconsistent_positions_fail() {
    let position = JournalPosition {
        acknowledged: 5,
        next_sequence: 9,
    };
    assert_eq!(position.pending().expect("valid position"), 3);
    let corrupt = JournalPosition {
        acknowledged: 9,
        next_sequence: 9,
    };
    assert!(corrupt.pending().is_err());
    let overflow = JournalPosition {
        acknowledged: u64::MAX,
        next_sequence: u64::MAX,
    };
    assert!(overflow.pending().is_err());
}
