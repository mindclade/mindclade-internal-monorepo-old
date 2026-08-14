use mindclade_artifact_proxy::ProxyHealth;
#[test]
fn drain_rejects_new_transfers() {
    let health = ProxyHealth::new();
    health.drain();
    assert!(!health.begin());
}
