// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_protocols::runtime::v1::{
    ArtifactGrant, ExecutionBudget, ExecutionTicketClaims, HeartbeatCommand, WorkerCommand,
    WorkerState, WorkerStatus, worker_command,
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
            readable_digests: vec![],
            writable_namespaces: vec!["runs/01".into()],
            maximum_read_bytes: 0,
            maximum_write_bytes: 1024,
            allow_range_reads: false,
            allow_multipart_writes: false,
        }),
        budget: Some(ExecutionBudget {
            cpu_millis: 1000,
            resident_memory_bytes: 1024,
            pinned_memory_bytes: 0,
            shared_memory_bytes: 0,
            local_disk_bytes: 0,
            open_file_descriptors: 4,
            object_store_requests: 1,
            queued_operations: 1,
            child_processes: 1,
            cpu_worker_threads: 1,
            gpu_memory_estimate_bytes: 0,
            checkpoint_staging_bytes: 0,
            telemetry_spool_bytes: 0,
            maximum_output_bytes: 1024,
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

#[test]
fn python_worker_command_golden_matches() {
    let command = WorkerCommand {
        sequence: 7,
        command: Some(worker_command::Command::Heartbeat(HeartbeatCommand {
            requested_at_unix_millis: 100,
        })),
    };
    assert_eq!(
        command.encode_to_vec(),
        [0x08, 0x07, 0x2a, 0x02, 0x08, 0x64]
    );
}

#[test]
fn python_worker_status_golden_matches() {
    let status = WorkerStatus {
        sequence: 11,
        ticket_id: "ticket".to_owned(),
        fencing_token: 9,
        state: WorkerState::Running as i32,
        observed_unix_millis: 100,
        message: "running".to_owned(),
        outputs: Vec::new(),
        diagnostic_artifact_digest: String::new(),
    };
    assert_eq!(
        status.encode_to_vec(),
        [
            0x08, 0x0b, 0x12, 0x06, 0x74, 0x69, 0x63, 0x6b, 0x65, 0x74, 0x18, 0x09, 0x20, 0x05,
            0x28, 0x64, 0x32, 0x07, 0x72, 0x75, 0x6e, 0x6e, 0x69, 0x6e, 0x67,
        ]
    );
}
