#![cfg(target_os = "linux")]

use mindclade_ipc_os::{BulkSegment, MemfdSegment};

#[test]
fn memfd_segment_round_trip_is_digest_checked() {
    let now = 1_000;
    let segment = MemfdSegment::create("test", b"payload", 1, "worker", now + 10_000, now).expect("segment");
    assert!(segment.raw_fd() >= 0);
    assert_eq!(segment.read_verified(1024, now).expect("read"), b"payload");
    segment.set_inheritable().expect("inheritable");
    segment.set_close_on_exec().expect("close-on-exec");
}
