// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_serving_runtime::bounded_stream;
use std::time::Duration;

#[test]
fn established_local_stream_survives_control_plane_unavailability() {
    let (sender, receiver) = bounded_stream(2, 16, 32).expect("bounded stream");
    sender.send(b"cached-policy".to_vec()).expect("data chunk");
    sender.finish(Vec::new()).expect("terminal chunk");

    let first = receiver
        .recv_timeout(Duration::from_millis(10))
        .expect("stream read")
        .expect("data chunk");
    assert_eq!(first.sequence, 1);
    assert_eq!(first.payload, b"cached-policy");
    assert!(!first.terminal);
    let terminal = receiver
        .recv_timeout(Duration::from_millis(10))
        .expect("stream read")
        .expect("terminal chunk");
    assert_eq!(terminal.sequence, 2);
    assert!(terminal.terminal);
}
