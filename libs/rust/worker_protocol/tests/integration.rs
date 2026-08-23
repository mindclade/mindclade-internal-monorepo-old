// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::hash_bytes;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector};
use mindclade_worker_protocol::*;
use std::collections::BTreeSet;

fn id(kind: &str, n: u8) -> ResourceId {
    format!("{kind}_019c00000000700080000000000000{n:02x}")
        .parse()
        .expect("id")
}

fn budget() -> ExecutionBudget {
    ExecutionBudget {
        resources: ResourceVector::new()
            .set(ResourceKind::CpuMillis, 1000)
            .set(ResourceKind::ResidentMemoryBytes, 1024)
            .set(ResourceKind::OpenFileDescriptors, 16)
            .set(ResourceKind::CpuThreads, 1),
        maximum_output_bytes: 10,
    }
}

#[test]
fn tickets_and_buffers_fail_closed() {
    let claims = ExecutionTicketClaims {
        ticket_id: id("ticket", 1),
        issuer: "control".into(),
        tenant_id: id("tenant", 2),
        workspace_id: id("workspace", 3),
        run_id: None,
        job_id: None,
        stage_id: Some(id("stage", 4)),
        request_id: None,
        attempt: 1,
        fencing_token: FencingToken::new(1).expect("fence"),
        model_bundle: None,
        engine_bundle: None,
        resolved_config_digest: hash_bytes(b"config"),
        reference_snapshot: None,
        artifacts: ArtifactGrant {
            readable_digests: BTreeSet::new(),
            writable_namespaces: BTreeSet::new(),
            maximum_read_bytes: 0,
            maximum_write_bytes: 0,
            allow_range_reads: false,
            allow_multipart_writes: false,
        },
        budget: budget(),
        execution_class: "cpu".into(),
        accelerator_capability: String::new(),
        not_before_unix_millis: 1,
        deadline_unix_millis: 100,
        expires_unix_millis: 90,
        policy_epoch: 1,
        route_snapshot_version: 1,
        revocation_epoch: 1,
        idempotency_key: String::new(),
    };
    let key = b"0123456789abcdef0123456789abcdef".to_vec();
    let payload = claims.canonical_bytes().expect("canonical");
    // Expected HMAC is produced independently in the Go/Python cross-language lane;
    // this test only proves malformed signatures fail closed without a verifier bypass.
    let verifier = HmacSha256Verifier::new([("k", key)]).expect("verifier");
    let ticket = ExecutionTicket {
        claims,
        signature: DetachedSignature {
            algorithm: "hmac-sha256".into(),
            key_id: "k".into(),
            value: vec![0; 32],
        },
    };
    let rev = RevocationSnapshot::empty(1, 0, 1000);
    assert!(ticket.validate(10, 1, 1, &rev, &verifier).is_err());
    let b = BufferDescriptor {
        segment_id: "s".into(),
        generation: 1,
        range: ByteRange::new(0, 1).expect("range"),
        element_type: "u8".into(),
        shape: vec![1],
        digest: hash_bytes(b"x"),
        owner_process: "p".into(),
        lease_expires_unix_millis: 20,
        access: BufferAccess::ReadOnly,
        transport: BufferTransport::LocalFile,
        locator: "/tmp/x".into(),
    };
    assert!(b.validate(10).is_ok());
    assert!(!payload.is_empty());
}

#[derive(Clone, Copy, Debug)]
struct AcceptingVerifier;

impl SignatureVerifier for AcceptingVerifier {
    fn verify(
        &self,
        _payload: &[u8],
        signature: &DetachedSignature,
    ) -> mindclade_faults::FaultResult<()> {
        signature.validate()
    }
}

fn fake_signature() -> DetachedSignature {
    DetachedSignature {
        algorithm: "test-signature".into(),
        key_id: "test-key".into(),
        value: vec![1; 32],
    }
}

#[test]
fn oversized_wire_budget_is_rejected_instead_of_clamped() {
    let budget = ExecutionBudget {
        resources: ResourceVector::new()
            .set(ResourceKind::CpuMillis, 1)
            .set(ResourceKind::ResidentMemoryBytes, 1)
            .set(ResourceKind::OpenFileDescriptors, u64::from(u32::MAX) + 1)
            .set(ResourceKind::CpuThreads, 1),
        maximum_output_bytes: 1,
    };
    assert!(budget.canonical_bytes().is_err());
}

#[test]
fn route_snapshot_survives_one_expired_member_route() {
    let now = 100;
    let revocations = RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1,
            created_unix_millis: 1,
            expires_unix_millis: 1_000,
            revoked_grant_ids: BTreeSet::new(),
            revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(),
            revoked_bundle_digests: BTreeSet::new(),
        },
        signature: fake_signature(),
    };
    let expired = DeploymentRoute {
        deployment_id: id("deployment", 10),
        model_bundle: hash_bytes(b"model-a"),
        engine_bundle: hash_bytes(b"engine-a"),
        endpoint: "unix:///runtime/a".into(),
        region: "us".into(),
        weight: 1,
        capabilities: BTreeSet::from(["structure".into()]),
        lease_expires_unix_millis: now,
        safety_policy: None,
    };
    let live = DeploymentRoute {
        deployment_id: id("deployment", 11),
        model_bundle: hash_bytes(b"model-b"),
        engine_bundle: hash_bytes(b"engine-b"),
        endpoint: "unix:///runtime/b".into(),
        region: "us".into(),
        weight: 1,
        capabilities: BTreeSet::from(["structure".into()]),
        lease_expires_unix_millis: 500,
        safety_policy: None,
    };
    let mut claims = RouteSnapshotClaims {
        snapshot_id: id("routesnap", 12),
        snapshot_digest: hash_bytes(b"placeholder"),
        version: 1,
        policy_epoch: 1,
        revocation_epoch: 1,
        created_unix_millis: 1,
        expires_unix_millis: 500,
        routes: vec![expired, live],
        minimum_runtime_version: "1.0.0".into(),
    };
    claims.snapshot_digest = claims.computed_digest().expect("route digest");
    let snapshot = RouteSnapshot {
        claims,
        signature: fake_signature(),
    };
    assert!(
        snapshot
            .validate(now, 1, 1, &revocations, &AcceptingVerifier)
            .is_ok()
    );
}

#[test]
fn signed_snapshots_are_rejected_before_their_activation_time() {
    let now = 100;
    let revocations = RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1,
            created_unix_millis: now + 1,
            expires_unix_millis: 1_000,
            revoked_grant_ids: BTreeSet::new(),
            revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(),
            revoked_bundle_digests: BTreeSet::new(),
        },
        signature: fake_signature(),
    };
    assert!(revocations.validate(now, 1, &AcceptingVerifier).is_err());

    let active_revocations = RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            created_unix_millis: 1,
            ..revocations.claims.clone()
        },
        signature: fake_signature(),
    };
    let route = DeploymentRoute {
        deployment_id: id("deployment", 13),
        model_bundle: hash_bytes(b"model"),
        engine_bundle: hash_bytes(b"engine"),
        endpoint: "unix:///runtime/model".into(),
        region: "us".into(),
        weight: 1,
        capabilities: BTreeSet::new(),
        lease_expires_unix_millis: 500,
        safety_policy: None,
    };
    let mut claims = RouteSnapshotClaims {
        snapshot_id: id("routesnap", 14),
        snapshot_digest: Digest::ZERO,
        version: 1,
        policy_epoch: 1,
        revocation_epoch: 1,
        created_unix_millis: now + 1,
        expires_unix_millis: 500,
        routes: vec![route],
        minimum_runtime_version: "1.0.0".into(),
    };
    claims.snapshot_digest = claims.computed_digest().expect("route digest");
    let snapshot = RouteSnapshot {
        claims,
        signature: fake_signature(),
    };
    assert!(
        snapshot
            .validate(now, 1, 1, &active_revocations, &AcceptingVerifier)
            .is_err()
    );
}

#[test]
fn worker_control_and_status_validation_are_bounded() {
    let claims = ExecutionTicketClaims {
        ticket_id: id("ticket", 21),
        issuer: "control".into(),
        tenant_id: id("tenant", 22),
        workspace_id: id("workspace", 23),
        run_id: None,
        job_id: None,
        stage_id: Some(id("stage", 24)),
        request_id: None,
        attempt: 1,
        fencing_token: FencingToken::new(1).expect("fence"),
        model_bundle: None,
        engine_bundle: None,
        resolved_config_digest: hash_bytes(b"config"),
        reference_snapshot: None,
        artifacts: ArtifactGrant {
            readable_digests: BTreeSet::new(),
            writable_namespaces: BTreeSet::new(),
            maximum_read_bytes: 0,
            maximum_write_bytes: 0,
            allow_range_reads: false,
            allow_multipart_writes: false,
        },
        budget: budget(),
        execution_class: "cpu".into(),
        accelerator_capability: String::new(),
        not_before_unix_millis: 1,
        deadline_unix_millis: 500,
        expires_unix_millis: 400,
        policy_epoch: 1,
        route_snapshot_version: 1,
        revocation_epoch: 1,
        idempotency_key: String::new(),
    };
    let ticket = ExecutionTicket {
        claims,
        signature: fake_signature(),
    };
    let command = WorkerCommand::Start {
        sequence: 1,
        ticket: Box::new(ticket),
        inputs: Vec::new(),
        operation: "preprocessing.msa".into(),
    };
    assert!(mindclade_worker_protocol::command::validate(&command, 100, 16).is_ok());
    let status = WorkerStatus {
        sequence: 1,
        ticket_id: id("ticket", 21).to_string(),
        fencing_token: FencingToken::new(1).expect("fence"),
        state: WorkerState::Running,
        observed_unix_millis: 100,
        message: "running".into(),
        outputs: Vec::new(),
        diagnostic_artifact: None,
    };
    assert!(mindclade_worker_protocol::status::validate(&status, 100, 16).is_ok());
    let invalid = WorkerCommand::Start {
        sequence: 2,
        ticket: match command {
            WorkerCommand::Start { ticket, .. } => ticket,
            WorkerCommand::Cancel { .. }
            | WorkerCommand::Drain { .. }
            | WorkerCommand::Heartbeat { .. } => unreachable!(),
        },
        inputs: Vec::new(),
        operation: "bad operation with spaces".into(),
    };
    assert!(mindclade_worker_protocol::command::validate(&invalid, 100, 16).is_err());
}
