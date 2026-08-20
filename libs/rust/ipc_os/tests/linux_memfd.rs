#![cfg(target_os = "linux")]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_ipc_os::{BulkSegment, MemfdSegment};
use std::io::Write;

#[test]
fn memfd_segment_round_trip_is_digest_checked() {
    let now = 1_000;
    let segment =
        MemfdSegment::create("test", b"payload", 1, "worker", now + 10_000, now).expect("segment");
    assert!(segment.raw_fd() >= 0);
    assert_eq!(segment.read_verified(1024, now).expect("read"), b"payload");
    let mut writable = std::fs::OpenOptions::new()
        .write(true)
        .open(format!("/proc/self/fd/{}", segment.raw_fd()))
        .expect("sealed memfd descriptor should be reopenable");
    assert!(writable.write_all(b"tampered").is_err());
    segment.set_inheritable().expect("inheritable");
    segment.set_close_on_exec().expect("close-on-exec");
}
