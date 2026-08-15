// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_node_agent::NodeHealth;
#[test]
fn drain_is_fail_closed() {
    let health = NodeHealth::new();
    health.drain();
    assert!(!health.snapshot().accepting);
}
