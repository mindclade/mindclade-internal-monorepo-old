// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::Code;
use mindclade_object_store::retry::retryable;
#[test]
fn retry_classification_is_explicit() {
    assert!(retryable(Code::Unavailable));
    assert!(!retryable(Code::InvalidArgument));
}
