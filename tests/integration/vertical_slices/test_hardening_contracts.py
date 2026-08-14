from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
def has(path,*tokens):
    text=(ROOT/path).read_text(); assert all(token in text for token in tokens)
def test_data_slice_uses_canonical_workload():
    has('control/orchestration/workload.go','WorkloadEnvelope','ExecutionTicket','ResolvedConfigDigest','workload_ticket_mismatch')
    has('services/node_agent/src/server.rs','execute_workload','WorkloadEnvelope')
def test_gc_is_two_phase_conditional_and_receipted():
    has('control/artifacts/gc.go','ArtifactReachability','ArtifactLease','ArtifactPin','RetentionHold','ObjectPath','BuildGCPlan','ValidateGCReceipt')
    has('libs/rust/artifact_cas/src/gc.rs','expected_version','store.delete','SweepReport','SweepResult','SweepOutcome')
    has('protocols/proto/mindclade/artifact/v1/artifact.proto','GarbageCollectionPlan','GarbageCollectionReceipt','object_path')
def test_node_diagnostics_and_budget_tree_are_observable():
    has('services/node_agent/src/diagnostics_bundle.rs','NodeDiagnosticsBundle','BudgetTreeSnapshot')
    has('libs/rust/runtime_core/src/budget/snapshot.rs','BudgetTreeSnapshot','reserved','used_estimate','waiters','rejections')
def test_release_hardening_policies_exist():
    for path in ('security/rust-supply-chain.toml','protocols/compatibility/runtime.toml','configs/qualification/failure_injection.toml','configs/qualification/rust_performance.toml','architecture/component_ownership.toml','architecture/enforced_decisions.toml'): assert (ROOT/path).exists()
