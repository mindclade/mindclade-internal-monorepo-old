// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteSize;
use mindclade_checkpoint_io::StagingBudget;
#[test]
fn staging_budget_rejects_overcommit() {
    let b = StagingBudget::new(ByteSize::new(10));
    let _r = b.reserve(ByteSize::new(8)).unwrap();
    assert!(b.reserve(ByteSize::new(3)).is_err());
}
