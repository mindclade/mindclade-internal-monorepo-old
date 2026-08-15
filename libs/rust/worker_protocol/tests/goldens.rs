// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_worker_protocol::sequence::Sequencer;
#[test]
fn protocol_sequence_rejects_replay() {
    let mut s = Sequencer::new();
    s.observe(1).unwrap();
    assert!(s.observe(1).is_err());
    s.observe(2).unwrap();
}
