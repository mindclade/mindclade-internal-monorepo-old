// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::hash_bytes;
use mindclade_gpu_host::{DeviceCapability, GpuHost, ModelSlotRequest};
use mindclade_runtime_core::{Budget, ResourceKind, ResourceVector};

#[test]
fn gpu_memory_is_budgeted() {
    let budget = Budget::root(
        "gpu",
        ResourceVector::new().set(ResourceKind::GpuMemoryEstimateBytes, 100),
    );
    let host = GpuHost::new(
        DeviceCapability {
            vendor: "nvidia".into(),
            architecture: "hopper".into(),
            total_memory_bytes: 100,
        },
        budget,
    )
    .unwrap();
    assert!(
        host.reserve_model(ModelSlotRequest {
            model_digest: hash_bytes(b"model"),
            minimum_memory_bytes: 80,
            pinned_memory_bytes: 0,
        })
        .is_ok()
    );
}

#[test]
fn zero_digest_and_zero_memory_are_rejected() {
    let budget = Budget::root(
        "gpu",
        ResourceVector::new().set(ResourceKind::GpuMemoryEstimateBytes, 100),
    );
    let host = GpuHost::new(
        DeviceCapability {
            vendor: "nvidia".into(),
            architecture: "hopper".into(),
            total_memory_bytes: 100,
        },
        budget,
    )
    .unwrap();
    assert!(
        host.reserve_model(ModelSlotRequest {
            model_digest: mindclade_content_digest::Digest::ZERO,
            minimum_memory_bytes: 1,
            pinned_memory_bytes: 0,
        })
        .is_err()
    );
}
