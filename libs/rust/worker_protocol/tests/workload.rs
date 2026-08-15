// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_worker_protocol::{WorkloadEnvelope, WorkloadKind};
#[test]
fn workload_types_are_public() {
    let _ = core::mem::size_of::<WorkloadEnvelope>();
    let _ = WorkloadKind::Ingestion;
}
