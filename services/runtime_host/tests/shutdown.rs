// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_runtime_host::HostHealth;

#[test]
fn drain_removes_readiness() {
    let health = HostHealth::new();
    health.set_accepting(true);
    health.set_gpu_ready(true);
    health.set_process_supervisor_ready(true);
    assert!(health.snapshot().ready());
    health.set_accepting(false);
    assert!(!health.snapshot().ready());
    assert!(health.snapshot().live());
}
