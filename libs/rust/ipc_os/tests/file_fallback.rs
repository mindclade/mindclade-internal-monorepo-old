use mindclade_ipc_os::file::FileSegment;
use std::time::{SystemTime, UNIX_EPOCH};

#[test]
fn file_fallback_round_trips_and_cleans_up() {
    let root = std::env::temp_dir().join(format!(
        "mindclade-ipc-file-{}",
        SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos()
    ));
    let segment = FileSegment::create(&root, "worker", 1, b"payload", u64::MAX).unwrap();
    assert_eq!(segment.read_verified(1).unwrap(), b"payload");
    let path = segment.descriptor().locator.clone();
    segment.remove().unwrap();
    assert!(!std::path::Path::new(&path).exists());
    let _ = std::fs::remove_dir(root);
}
