// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteSize;
use mindclade_runtime_core::ManualClock;
use mindclade_telemetry_spool::{SpoolConfig, TelemetrySpool};
use std::fs;
use std::sync::Arc;
use std::time::{Instant, SystemTime};

#[test]
fn append_replay_acknowledge_and_compact() {
    let root = std::env::temp_dir().join(format!("mindclade-spool-{}", std::process::id()));
    let _ = fs::remove_dir_all(&root);
    let config = SpoolConfig {
        maximum_event_bytes: ByteSize::new(1024),
        maximum_segment_bytes: ByteSize::new(2048),
        maximum_total_bytes: ByteSize::new(8192),
    };
    let spool = TelemetrySpool::open(
        &root,
        config,
        Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now())),
    );
    assert!(spool.is_ok());
    if let Ok(spool) = spool {
        let first = spool.append("training.step", b"one");
        let second = spool.append("training.step", b"two");
        assert!(first.is_ok() && second.is_ok());
        assert_eq!(
            spool.replay_after(0, 10).ok().map(|events| events.len()),
            Some(2)
        );
        if let Ok(second) = second {
            assert!(spool.acknowledge(second.sequence).is_ok());
            assert!(spool.compact().is_ok());
        }
    }
    let _ = fs::remove_dir_all(root);
}
