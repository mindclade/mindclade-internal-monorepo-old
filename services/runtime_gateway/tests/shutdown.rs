use mindclade_runtime_gateway::GatewayHealth;

#[test]
fn drain_removes_readiness_before_process_shutdown() {
    let health = GatewayHealth::new();
    health.set_accepting(true);
    health.set_policy_fresh(true);
    health.set_runtime_host_ready(true);
    assert!(health.snapshot().ready());
    health.set_accepting(false);
    assert!(!health.snapshot().ready());
    assert!(health.snapshot().live());
}
