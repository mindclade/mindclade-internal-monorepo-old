// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bounded_parse::{AllocationBudget, Limits};
#[test]
fn allocation_budget_is_bounded() {
    let mut b = AllocationBudget::from_limits(Limits::default());
    assert!(b.charge(1).is_ok());
    assert!(b.used() > 0);
}
