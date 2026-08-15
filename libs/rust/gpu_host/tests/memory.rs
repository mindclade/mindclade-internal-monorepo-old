// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_gpu_host::memory::MemorySnapshot;

#[test]
fn memory_snapshot_enforces_physical_invariants() {
    let snapshot = MemorySnapshot::new(100, 25).expect("valid snapshot");
    assert_eq!(snapshot.available(), 75);
    assert_eq!(snapshot.pressure_permyriad(), 2_500);
    assert!(MemorySnapshot::new(0, 0).is_err());
    assert!(MemorySnapshot::new(100, 101).is_err());
}
