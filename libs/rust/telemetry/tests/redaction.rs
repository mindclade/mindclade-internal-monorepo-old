// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_telemetry::{AttributeValue, Attributes};
#[test]
fn redacted_value_never_contains_plaintext() {
    let mut a = Attributes::new();
    assert!(a.insert_redacted("secret"));
    let text = a.iter().next().unwrap().1.to_string();
    assert_eq!(text, "[REDACTED]");
    assert!(matches!(
        a.iter().next().unwrap().1,
        AttributeValue::Redacted
    ));
}
