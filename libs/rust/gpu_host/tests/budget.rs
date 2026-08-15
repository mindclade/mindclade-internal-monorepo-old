// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_gpu_host::budget::gpu_budget;
use mindclade_runtime_core::ResourceKind;
#[test]
fn gpu_budget_tracks_device_and_pinned_memory() {
    let b = gpu_budget(100, 20);
    assert_eq!(b.get(ResourceKind::GpuMemoryEstimateBytes), 100);
    assert_eq!(b.get(ResourceKind::PinnedMemoryBytes), 20);
}
