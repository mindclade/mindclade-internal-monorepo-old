use mindclade_protocols::runtime::v1::{
    ArtifactGrant, ExecutionBudget, ExecutionTicketClaims
};
use prost::Message;

#[test]
fn execution_ticket_claims_round_trip() {
    let claims = ExecutionTicketClaims {
        ticket_id: "ticket_01".into(),
        issuer: "control-plane".into(),
        tenant_id: "tenant_01".into(),
        workspace_id: "workspace_01".into(),
        run_id: String::new(),
        job_id: String::new(),
        stage_id: "stage_01".into(),
        request_id: "request_01".into(),
        attempt: 1,
        fencing_token: 9,
        model_bundle_digest: String::new(),
        engine_bundle_digest: "sha256:engine".into(),
        resolved_config_digest: "sha256:config".into(),
        reference_snapshot_digest: String::new(),
        artifacts: Some(ArtifactGrant {
            readable_digests: vec![], writable_namespaces: vec!["runs/01".into()], maximum_read_bytes: 0, maximum_write_bytes: 1024,
            allow_range_reads: false, allow_multipart_writes: false
        }),
        budget: Some(ExecutionBudget {
            cpu_millis: 1000, resident_memory_bytes: 1024, pinned_memory_bytes: 0, shared_memory_bytes: 0, local_disk_bytes: 0,
            open_file_descriptors: 4, object_store_requests: 1, queued_operations: 1, child_processes: 1, cpu_worker_threads: 1,
            gpu_memory_estimate_bytes: 0, checkpoint_staging_bytes: 0, telemetry_spool_bytes: 0, maximum_output_bytes: 1024
        }),
        execution_class: "cpu".into(),
        accelerator_capability: String::new(),
        not_before_unix_millis: 1,
        deadline_unix_millis: 2,
        expires_unix_millis: 3,
        policy_epoch: 1,
        route_snapshot_version: 1,
        revocation_epoch: 1,
        idempotency_key: "request_01".into(),
    };
    let encoded = claims.encode_to_vec();
    let decoded = ExecutionTicketClaims::decode(encoded.as_slice()).expect("decode");
    assert_eq!(decoded, claims);
}
