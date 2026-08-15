// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_runtime_core::FencingToken;
use mindclade_worker_runtime::commit::require_current;
#[test]
fn stale_fence_is_rejected() {
    let one = FencingToken::new(1).unwrap();
    let two = FencingToken::new(2).unwrap();
    assert!(require_current(one, two).is_err());
    assert!(require_current(two, two).is_ok());
}
