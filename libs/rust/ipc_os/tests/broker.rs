use mindclade_ipc_os::{BulkBackend, BulkBufferBroker};
use std::time::{SystemTime, UNIX_EPOCH};

#[test]
fn broker_is_bounded_and_reaps_expired_segments() {
    let root = std::env::temp_dir().join(format!(
        "mindclade-ipc-broker-{}",
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time")
            .as_nanos()
    ));
    let backend = BulkBackend::local_file(&root).expect("backend");
    let broker = BulkBufferBroker::with_backend(backend, 1, 1024).expect("broker");
    let descriptor = broker
        .publish("a", b"payload", 1, "worker", 10, 1)
        .expect("publish");
    assert_eq!(broker.active(), 1);
    assert!(broker.publish("b", b"payload", 2, "worker", 10, 1).is_err());
    assert_eq!(
        broker
            .read_verified(&descriptor.segment_id, 1024, 1)
            .expect("read"),
        b"payload"
    );
    assert_eq!(broker.reap_expired(10), 1);
    assert_eq!(broker.active(), 0);
    let _ = std::fs::remove_dir(root);
}
