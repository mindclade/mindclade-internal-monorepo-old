// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_ipc::ProtocolVersion;
#[test]
fn protocol_negotiates_minor_down() {
    assert_eq!(
        ProtocolVersion::negotiate(
            ProtocolVersion { major: 1, minor: 4 },
            ProtocolVersion { major: 1, minor: 2 }
        )
        .unwrap()
        .minor,
        2
    );
}
