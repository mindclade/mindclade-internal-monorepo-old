// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Lease expiry and checkpoint atomicity under an interrupted worker.
//!
//! Both cases here are named by qualification gates in
//! `configs/qualification/failure_injection.toml` -- `lease_expires_during_preprocessing`
//! (invariant `no_stale_fenced_commit`) and `checkpoint_upload_interrupted` (invariant
//! `atomic_checkpoint_commit`). What they used to contain was
//!
//!     let stale = FencingToken::new(7)?; let current = FencingToken::new(8)?;
//!     assert!(stale < current);
//!
//! and
//!
//!     let unpublished_generation: Option<u64> = None;
//!     assert!(unpublished_generation.is_none());
//!
//! -- that is, `7 < 8` and `None.is_none()`. Neither ran a lease, a commit, or a
//! checkpoint. Both gates were green by construction and would have stayed green if the
//! fencing check and the commit marker were both deleted.
//!
//! The cases below drive the real state machine and the real checkpoint writer/reader, and
//! each asserts both directions: the stale actor is refused *and* the current one is
//! allowed, so a mechanism that refused everything would not pass either.

use mindclade_artifact_cas::{ArtifactCas, CasConfig};
use mindclade_checkpoint_io::{CheckpointReader, CheckpointWriter, verify::require_valid};
use mindclade_content_digest::hash_bytes;
use mindclade_faults::Code;
use mindclade_identifiers::ResourceId;
use mindclade_object_store::{MemoryStore, ObjectPath, ObjectStore, PutCondition};
use mindclade_runtime_core::{Budget, FencingToken, ManualClock, ResourceKind, ResourceVector};
use mindclade_worker_protocol::{
    ArtifactGrant, DetachedSignature, ExecutionBudget, ExecutionTicket, ExecutionTicketClaims,
    RevocationSnapshot, SignatureVerifier, WorkerState,
};
use mindclade_worker_runtime::WorkerRuntime;
use mindclade_worker_runtime::lease::LeaseIdentity;
use std::collections::BTreeSet;
use std::sync::Arc;
use std::time::{Instant, SystemTime};

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

fn id(kind: &str, n: u8) -> ResourceId {
    format!("{kind}_019c00000000700080000000000000{n:02x}")
        .parse()
        .expect("resource id")
}

fn resources() -> ResourceVector {
    ResourceVector::new()
        .set(ResourceKind::CpuMillis, 10_000)
        .set(ResourceKind::ResidentMemoryBytes, 16 * 1024 * 1024)
        .set(ResourceKind::CheckpointStagingBytes, 16 * 1024 * 1024)
        .set(ResourceKind::OpenFileDescriptors, 64)
        .set(ResourceKind::ObjectStoreRequests, 64)
        .set(ResourceKind::QueuedRequests, 64)
        .set(ResourceKind::Processes, 4)
        .set(ResourceKind::CpuThreads, 4)
}

/// One attempt at the same stage: same ticket, new fence, incremented attempt counter.
fn ticket(attempt: u32, fence: u64, expires_unix_millis: u64) -> ExecutionTicket {
    ExecutionTicket {
        claims: ExecutionTicketClaims {
            ticket_id: id("ticket", 1),
            issuer: "control".into(),
            tenant_id: id("tenant", 2),
            workspace_id: id("workspace", 3),
            run_id: None,
            job_id: Some(id("job", 4)),
            stage_id: Some(id("stage", 5)),
            request_id: None,
            attempt,
            fencing_token: FencingToken::new(fence).expect("fence"),
            model_bundle: None,
            engine_bundle: None,
            resolved_config_digest: hash_bytes(b"config"),
            reference_snapshot: None,
            artifacts: ArtifactGrant {
                readable_digests: BTreeSet::new(),
                writable_namespaces: BTreeSet::from(["checkpoint-output".to_owned()]),
                maximum_read_bytes: 16 * 1024 * 1024,
                maximum_write_bytes: 16 * 1024 * 1024,
                allow_range_reads: true,
                allow_multipart_writes: false,
            },
            budget: ExecutionBudget {
                resources: resources(),
                maximum_output_bytes: 16 * 1024 * 1024,
            },
            execution_class: "cpu".into(),
            accelerator_capability: String::new(),
            not_before_unix_millis: 1,
            deadline_unix_millis: 100_000,
            expires_unix_millis,
            policy_epoch: 1,
            route_snapshot_version: 1,
            revocation_epoch: 1,
            idempotency_key: "failure-injection".into(),
        },
        signature: DetachedSignature {
            algorithm: "test-signature".into(),
            key_id: "test-key".into(),
            value: vec![1; 32],
        },
    }
}

fn runtime() -> WorkerRuntime {
    WorkerRuntime::new(Budget::root("node", resources()))
}

fn lease(runtime: &WorkerRuntime, ticket: &ExecutionTicket, now: u64) {
    runtime
        .lease(
            ticket,
            now,
            1,
            1,
            &RevocationSnapshot::empty(1, 1, 30_000),
            &AcceptingVerifier,
        )
        .expect("lease");
}

#[test]
fn lease_expiry_rejects_stale_commit() {
    // A worker takes the stage under fence 7 and starts preprocessing. Its lease expires
    // mid-stage; the control plane hands the same ticket to a replacement attempt under
    // fence 8. The invariant is that the first worker -- which has no way of knowing it
    // lost the lease, and may still be holding output it is about to publish -- cannot
    // commit anything.
    let runtime = runtime();
    runtime.start().expect("start");

    let stale_fence = FencingToken::new(7).expect("stale fence");
    let current_fence = FencingToken::new(8).expect("current fence");

    lease(&runtime, &ticket(1, stale_fence.get(), 9_000), 1_000);
    runtime.run().expect("run");
    assert_eq!(runtime.state(), WorkerState::Running);

    // While the stage is running, the lease reaches its expiry. The lease identity is what
    // the control plane fences on, so it is checked here rather than inferred.
    let identity = LeaseIdentity {
        ticket_id: id("ticket", 1),
        fencing_token: stale_fence,
        expires_unix_millis: 5_000,
    };
    identity.validate(4_999).expect("lease is live before expiry");
    let expired = identity
        .validate(5_000)
        .expect_err("an expired lease must not validate");
    assert_eq!(expired.code(), Code::FailedPrecondition);

    // The node abandons the attempt and re-leases the stage under the new fence. This is
    // the fence rotation the stale worker is about to lose to.
    runtime.begin_recovery().expect("begin recovery");
    runtime.recovered().expect("recovered");
    lease(&runtime, &ticket(2, current_fence.get(), 20_000), 6_000);
    runtime.run().expect("run replacement");

    // The stale attempt returns and tries to publish. Both commit entry points are fenced,
    // so both must refuse, and the worker must not have moved toward a terminal state.
    let refused = runtime
        .begin_commit(stale_fence)
        .expect_err("a stale fence must not open a commit");
    assert_eq!(refused.code(), Code::Conflict);
    assert_eq!(runtime.state(), WorkerState::Running);

    let refused = runtime
        .complete(stale_fence)
        .expect_err("a stale fence must not complete a stage");
    assert_eq!(refused.code(), Code::Conflict);
    assert_eq!(runtime.state(), WorkerState::Running);

    // The other direction: the current fence still commits. Without this, a runtime that
    // refused every commit would satisfy the assertions above.
    runtime.begin_commit(current_fence).expect("current commit");
    runtime.complete(current_fence).expect("current completion");
    assert_eq!(runtime.state(), WorkerState::Completed);
}

#[test]
fn checkpoint_interruption_preserves_atomicity() {
    // Checkpoint publication is two objects: the manifest and the COMMITTED marker that
    // names its digest. An upload interrupted between them must not be readable as a
    // checkpoint -- that is the whole content of "atomic".
    let store = Arc::new(MemoryStore::new());
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let cas = ArtifactCas::new(store.clone(), clock.clone(), CasConfig::default()).expect("cas");
    let writer = CheckpointWriter::new(cas.clone(), store.clone(), clock);
    let reader = CheckpointReader::new(cas, store.clone());

    // Attempt one: two shards are uploaded, then the worker is killed before commit.
    let interrupted_id = {
        let mut session = writer
            .begin(id("run", 6), 42, 2, hash_bytes(b"parallel-plan"))
            .expect("begin");
        let interrupted_id = session.checkpoint_id().to_string();
        session
            .write_shard("rank-0.safetensors", 0, b"shard-zero")
            .expect("first shard");
        session
            .write_shard("rank-1.safetensors", 1, b"shard-one")
            .expect("second shard");
        // Dropped without commit: this is the kill.
        interrupted_id
    };

    let fault = reader
        .load(&interrupted_id)
        .expect_err("an uncommitted checkpoint must not load");
    assert_eq!(fault.code(), Code::NotFound);

    // The narrower interruption window: the manifest object landed but the marker did not.
    // The shards are all present in the CAS and the manifest is byte-for-byte valid, so
    // nothing but the marker distinguishes this from a complete checkpoint.
    let partial_id = {
        let mut session = writer
            .begin(id("run", 6), 43, 2, hash_bytes(b"parallel-plan"))
            .expect("begin");
        let partial_id = session.checkpoint_id().to_string();
        session
            .write_shard("rank-0.safetensors", 0, b"shard-zero")
            .expect("first shard");
        session
            .write_shard("rank-1.safetensors", 1, b"shard-one")
            .expect("second shard");
        let manifest = session.commit().expect("commit");
        let marker = ObjectPath::new(format!("checkpoints/{partial_id}/COMMITTED"))
            .expect("marker path");
        assert!(
            store.delete(&marker, None).expect("delete marker"),
            "the marker must exist before it is removed"
        );
        assert_eq!(manifest.shards.len(), 2);
        partial_id
    };

    let fault = reader
        .load(&partial_id)
        .expect_err("a manifest without its commit marker must not load");
    assert_eq!(fault.code(), Code::FailedPrecondition);

    // Attempt two runs to completion. The same shard content is uploaded, so this also
    // shows the interrupted attempt left no lock or half-state that blocks recovery.
    let committed_id = {
        let mut session = writer
            .begin(id("run", 6), 44, 2, hash_bytes(b"parallel-plan"))
            .expect("begin");
        let committed_id = session.checkpoint_id().to_string();
        session
            .write_shard("rank-0.safetensors", 0, b"shard-zero")
            .expect("first shard");
        session
            .write_shard("rank-1.safetensors", 1, b"shard-one")
            .expect("second shard");
        session.commit().expect("commit");
        committed_id
    };

    let report = require_valid(&reader, &committed_id).expect("committed checkpoint verifies");
    assert_eq!(report.verified_shards, 2);
    assert!(report.failures.is_empty());

    // A tampered marker is data loss, not a readable checkpoint: the marker binds the
    // manifest bytes, so pointing it at a different digest must be refused rather than
    // silently accepted.
    let marker =
        ObjectPath::new(format!("checkpoints/{committed_id}/COMMITTED")).expect("marker path");
    store.delete(&marker, None).expect("delete marker");
    store
        .put(
            &marker,
            hash_bytes(b"not-the-manifest").to_string().as_bytes(),
            PutCondition::CreateOnly,
        )
        .expect("rewrite marker");
    let fault = reader
        .load(&committed_id)
        .expect_err("a marker that does not match the manifest must not load");
    assert_eq!(fault.code(), Code::DataLoss);
}
