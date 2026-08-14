use mindclade_worker_runtime::drain::DrainRequest;
#[test]fn drain_requires_future_deadline() {
    assert!(DrainRequest {
        reason: "deploy".into(), deadline_unix_millis: 10
    }.validate(9).is_ok());
    assert!(DrainRequest {
        reason: "deploy".into(), deadline_unix_millis: 9
    }.validate(9).is_err());
}
