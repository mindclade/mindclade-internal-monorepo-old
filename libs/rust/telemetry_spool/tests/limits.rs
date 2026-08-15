// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_telemetry_spool::DeliveryBatch;
#[test]
fn empty_batch_is_valid() {
    let b = DeliveryBatch::new(Vec::new(), 0).unwrap();
    assert!(b.highest_sequence().is_none());
}
