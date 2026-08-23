// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Behaviour of the workload envelope after ADR-0023 split identity from placement.
//!
//! This file previously asserted `size_of::<WorkloadEnvelope>()` and one enum variant, which is
//! why the type could carry `Vec<BufferDescriptor>` under the wire's `inputs` name for as long
//! as it did: nothing here could tell the difference.

use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::hash_bytes;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector};
use mindclade_worker_protocol::*;
use std::collections::BTreeSet;

const NOW: u64 = 10;

fn id(kind: &str, n: u8) -> ResourceId {
    format!("{kind}_019c00000000700080000000000000{n:02x}")
        .parse()
        .expect("id")
}

fn artifact(payload: &[u8]) -> ArtifactRef {
    ArtifactRef {
        digest: hash_bytes(payload),
        size_bytes: payload.len() as u64,
        media_type: "application/octet-stream".into(),
        logical_kind: "dataset-shard".into(),
        schema_version: 1,
    }
}

fn ticket() -> ExecutionTicket {
    ExecutionTicket {
        claims: ExecutionTicketClaims {
            ticket_id: id("ticket", 1),
            issuer: "control".into(),
            tenant_id: id("tenant", 2),
            workspace_id: id("workspace", 3),
            run_id: Some(id("run", 4)),
            job_id: Some(id("job", 5)),
            stage_id: Some(id("stage", 6)),
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
            budget: ExecutionBudget {
                resources: ResourceVector::new()
                    .set(ResourceKind::CpuMillis, 1000)
                    .set(ResourceKind::ResidentMemoryBytes, 1024)
                    .set(ResourceKind::OpenFileDescriptors, 16)
                    .set(ResourceKind::CpuThreads, 1),
                maximum_output_bytes: 10,
            },
            execution_class: "cpu".into(),
            accelerator_capability: String::new(),
            not_before_unix_millis: 1,
            deadline_unix_millis: 1_000,
            expires_unix_millis: 900,
            policy_epoch: 1,
            route_snapshot_version: 1,
            revocation_epoch: 1,
            idempotency_key: String::new(),
        },
        signature: DetachedSignature {
            algorithm: "hmac-sha256".into(),
            key_id: "k".into(),
            value: vec![1],
        },
    }
}

fn envelope() -> WorkloadEnvelope {
    WorkloadEnvelope {
        workload_id: id("workload", 7),
        run_id: id("run", 4),
        job_id: id("job", 5),
        stage_id: id("stage", 6),
        attempt: 1,
        tenant_id: id("tenant", 2),
        workspace_id: id("workspace", 3),
        execution_ticket: ticket(),
        inputs: vec![artifact(b"input")],
        expected_outputs: vec![artifact(b"output")],
        resolved_config_digest: hash_bytes(b"config"),
        resource_class: "cpu-highmem".into(),
        created_unix_millis: 5,
        deadline_unix_millis: 900,
        stage_kind: WorkloadKind::Preprocess,
        operation: "featurize".into(),
    }
}

fn buffer(payload: &[u8]) -> BufferDescriptor {
    BufferDescriptor {
        segment_id: "s".into(),
        generation: 1,
        range: ByteRange::new(0, 1).expect("range"),
        element_type: "u8".into(),
        shape: vec![1],
        digest: hash_bytes(payload),
        owner_process: "p".into(),
        lease_expires_unix_millis: 500,
        access: BufferAccess::ReadOnly,
        transport: BufferTransport::LocalFile,
        locator: "/tmp/x".into(),
    }
}

#[test]
fn a_well_formed_envelope_validates() {
    assert!(envelope().validate(NOW).is_ok());
}

#[test]
fn inputs_are_content_identity_not_placement() {
    // The wire declares `repeated ArtifactRef inputs`. A reference names bytes and carries no
    // lease, segment or transport, so it stays true after any buffer it was materialized into
    // has expired. This is the property the old `Vec<BufferDescriptor>` could not hold.
    let envelope = envelope();
    assert_eq!(envelope.inputs[0].digest, hash_bytes(b"input"));
    assert_eq!(envelope.expected_outputs[0].digest, hash_bytes(b"output"));
}

#[test]
fn an_artifact_reference_must_declare_a_positive_schema_version() {
    // proto3 cannot distinguish an absent uint32 from zero, so zero would let an unset field
    // pass as a declared contract version.
    let mut reference = artifact(b"input");
    reference.schema_version = 0;
    assert!(reference.validate().is_err());
}

#[test]
fn materialized_buffers_must_be_authorized_by_the_envelope() {
    let envelope = envelope();
    assert!(envelope.bind_materialized(&[buffer(b"input")], NOW).is_ok());
    // A buffer for content the envelope never listed. Before the split the node had no
    // authorized set to compare against, so this was indistinguishable from a declared input.
    assert!(
        envelope
            .bind_materialized(&[buffer(b"not-an-input")], NOW)
            .is_err()
    );
}

#[test]
fn an_expired_materialized_lease_is_still_refused() {
    let envelope = envelope();
    let mut stale = buffer(b"input");
    stale.lease_expires_unix_millis = NOW;
    assert!(envelope.bind_materialized(&[stale], NOW).is_err());
}

#[test]
fn every_identity_the_envelope_duplicates_is_checked_against_the_signed_ticket() {
    // Only the ticket is signed. Rust used to compare three of these seven, so an envelope
    // could name one tenant while its signed authority named another and still validate; Go
    // compared all seven. Each mutation below must be refused on its own.
    for mutate in [
        (|envelope: &mut WorkloadEnvelope| envelope.tenant_id = id("tenant", 0x20)) as fn(&mut _),
        |envelope: &mut WorkloadEnvelope| envelope.workspace_id = id("workspace", 0x21),
        |envelope: &mut WorkloadEnvelope| envelope.run_id = id("run", 0x22),
        |envelope: &mut WorkloadEnvelope| envelope.job_id = id("job", 0x23),
        |envelope: &mut WorkloadEnvelope| envelope.stage_id = id("stage", 0x24),
        |envelope: &mut WorkloadEnvelope| envelope.attempt = 2,
        |envelope: &mut WorkloadEnvelope| {
            envelope.resolved_config_digest = hash_bytes(b"other-config");
        },
    ] {
        let mut envelope = envelope();
        mutate(&mut envelope);
        assert!(
            envelope.validate(NOW).is_err(),
            "an envelope field that contradicts the signed ticket was accepted"
        );
    }
}

#[test]
fn a_ticket_without_the_envelopes_work_identity_is_refused() {
    let mut envelope = envelope();
    envelope.execution_ticket.claims.run_id = None;
    assert!(envelope.validate(NOW).is_err());
}
