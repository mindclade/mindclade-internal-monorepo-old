// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_ingestion_worker::{IngestionWorkerConfig, Lifecycle};

#[test]
fn configuration_and_lifecycle_fail_closed() {
    assert!(
        IngestionWorkerConfig { maximum_outputs: 0 }
            .validate()
            .is_err()
    );
    assert!(
        IngestionWorkerConfig {
            maximum_outputs: 4_096
        }
        .validate()
        .is_ok()
    );

    let mut lifecycle = Lifecycle::Ready;
    assert!(lifecycle.can_accept());
    lifecycle.drain();
    assert_eq!(lifecycle, Lifecycle::Draining);
    assert!(!lifecycle.can_accept());
}
