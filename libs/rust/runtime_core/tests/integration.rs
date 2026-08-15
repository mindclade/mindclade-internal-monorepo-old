// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::hash_bytes;
use mindclade_runtime_core::{
    Budget, CancellationToken, FencingToken, Precondition, ResourceKind, ResourceVector,
    ResourceVersion, TaskGroup,
};

#[test]
fn budgets_are_hierarchical_and_fencing_rejects_stale_work() {
    let root = Budget::root(
        "node",
        ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 100),
    );
    let worker = Budget::child(
        root.clone(),
        "worker",
        ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 80),
    );
    let reservation =
        worker.reserve(ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 64));
    assert!(reservation.is_ok());
    assert!(
        worker
            .reserve(ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 32))
            .is_err()
    );
    let current = FencingToken::new(2).expect("token");
    assert!(
        FencingToken::new(1)
            .expect("token")
            .require_current(current)
            .is_err()
    );
    drop(reservation);
}

#[test]
fn compatibility_primitives_remain_deterministic() {
    let version = ResourceVersion::new(1, hash_bytes(b"x"));
    assert!(Precondition::Match(version).check(Some(version)).is_ok());
    let token = CancellationToken::new();
    let group = TaskGroup::new(token);
    group.spawn("unit", |_| Ok(())).expect("spawn");
    assert!(group.join_all().is_clean());
}
