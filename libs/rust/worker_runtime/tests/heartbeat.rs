use mindclade_worker_runtime::heartbeat::Heartbeat;

#[test]
fn heartbeat_rejects_future_clock_domain_and_detects_staleness() {
    let heartbeat = Heartbeat {
        sequence: 1,
        observed_unix_millis: 100,
        progress_sequence: 4,
    };
    assert!(!heartbeat.is_stale(150, 100).expect("fresh heartbeat"));
    assert!(heartbeat.is_stale(250, 100).expect("stale heartbeat"));
    assert!(heartbeat.is_stale(99, 100).is_err());
}
