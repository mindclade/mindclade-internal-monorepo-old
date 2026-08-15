// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bounded_parse::{Cursor, Limits};

#[test]
fn final_unterminated_line_is_returned() {
    let mut c = Cursor::new(b"abc", Limits::default()).unwrap();
    assert_eq!(c.next_line().unwrap().unwrap().1, b"abc");
    assert!(c.next_line().unwrap().is_none());
}
