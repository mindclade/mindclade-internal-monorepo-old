use mindclade_node_agent::NodeHealth;
#[test] fn drain_is_fail_closed() { let health=NodeHealth::new(); health.drain(); assert!(!health.snapshot().accepting); }
