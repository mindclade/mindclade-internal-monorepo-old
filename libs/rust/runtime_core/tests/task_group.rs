// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_runtime_core::{CancellationToken, TaskGroup};

#[test]
fn owned_tasks_join_cleanly() {
    let group = TaskGroup::new(CancellationToken::new());
    group.spawn("worker", |_| Ok(())).unwrap();
    let r = group.join_all();
    assert_eq!(r.joined, 1);
    assert!(r.is_clean());
}
