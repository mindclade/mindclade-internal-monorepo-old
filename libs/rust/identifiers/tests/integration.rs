// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_identifiers::{Name, ResourceId};
use mindclade_runtime_core::ManualClock;
use std::time::{Instant, SystemTime};

#[test]
fn generated_ids_round_trip_and_do_not_repeat() {
    let clock = ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now());
    let first = ResourceId::generate("run", &clock).expect("id");
    let second = ResourceId::generate("run", &clock).expect("id");
    assert_ne!(first, second);
    assert_eq!(first.to_string().parse::<ResourceId>().ok(), Some(first));
}

#[test]
fn parses_go_compatible_uuidv7() {
    let id = "run_019c0000000070008000000000000001"
        .parse::<ResourceId>()
        .expect("valid");
    assert_eq!(id.kind(), "run");
    assert_eq!(id.to_string(), "run_019c0000000070008000000000000001");
}

#[test]
fn names_reject_traversal() {
    assert!(Name::new("datasets/../secret").is_err());
    assert!(Name::new("datasets/shard-0001").is_ok());
}
