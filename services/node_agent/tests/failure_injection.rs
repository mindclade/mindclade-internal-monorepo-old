// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Named failure-injection gates for the node agent.
//!
//! `configs/qualification/failure_injection.toml` runs these two functions by
//! name as the `lease_expires_during_preprocessing` and
//! `checkpoint_upload_interrupted` scenarios, asserting the
//! `no_stale_fenced_commit` and `atomic_checkpoint_commit` invariants.
//!
//! What they used to assert was `7 < 8` and `Option::<u64>::None.is_none()`.
//! Both passed unconditionally, neither touched the node agent, and a gate that
//! cannot fail is not a gate. They now drive the real admission path, the real
//! fencing check, and the real content-addressed commit.

use mindclade_artifact_cas::{ArtifactCas, CasConfig};
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, FaultResult};
use mindclade_identifiers::{Name, ResourceId};
use mindclade_manifests::{ArtifactManifest, BlobRef};
use mindclade_node_agent::{
    NodeAgentConfig, NodeAgentCore, NodeHealth, StageContext, StageExecutor, StageFuture,
};
use mindclade_object_store::LocalStore;
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector, SystemClock};
use mindclade_worker_protocol::{
    ArtifactGrant, BufferDescriptor, DetachedSignature, ExecutionBudget, ExecutionTicket,
    ExecutionTicketClaims, RevocationSnapshot, SignatureVerifier,
};
use mindclade_worker_runtime::WorkerRuntime;
use std::collections::BTreeSet;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

#[derive(Clone, Copy, Debug)]
struct AcceptingVerifier;

impl SignatureVerifier for AcceptingVerifier {
    fn verify(&self, _payload: &[u8], signature: &DetachedSignature) -> FaultResult<()> {
        signature.validate()
    }
}

/// A stage that must never be reached. Reaching it means admission let an
/// expired lease through, which is the defect these gates exist to catch.
#[derive(Clone, Copy, Debug)]
struct UnreachableExecutor;

impl StageExecutor for UnreachableExecutor {
    fn execute<'a>(
        &'a self,
        _operation: &'a str,
        _inputs: &'a [BufferDescriptor],
        _ticket: &'a ExecutionTicket,
        _context: &'a StageContext,
    ) -> StageFuture<'a> {
        Box::pin(async { panic!("an expired lease must never reach stage execution") })
    }
}

fn id(kind: &str, n: u8) -> ResourceId {
    format!("{kind}_019c00000000700080000000000000{n:02x}")
        .parse()
        .expect("resource id")
}

fn ticket(fencing_token: u64, expires_unix_millis: u64) -> ExecutionTicket {
    let resources = ResourceVector::new()
        .set(ResourceKind::CpuMillis, 10_000)
        .set(ResourceKind::ResidentMemoryBytes, 16 * 1024 * 1024)
        .set(ResourceKind::OpenFileDescriptors, 64)
        .set(ResourceKind::CpuThreads, 4);
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
            attempt: 1,
            fencing_token: FencingToken::new(fencing_token).expect("fence"),
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
                resources,
                maximum_output_bytes: 16 * 1024 * 1024,
            },
            execution_class: "cpu".into(),
            accelerator_capability: String::new(),
            not_before_unix_millis: 1,
            deadline_unix_millis: expires_unix_millis.saturating_add(1_000),
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

fn core(health: Arc<NodeHealth>) -> NodeAgentCore {
    let config = NodeAgentConfig {
        node_resources: ResourceVector::new()
            .set(ResourceKind::CpuMillis, 100_000)
            .set(ResourceKind::ResidentMemoryBytes, 128 * 1024 * 1024)
            .set(ResourceKind::LocalDiskBytes, 128 * 1024 * 1024)
            .set(ResourceKind::OpenFileDescriptors, 1_024)
            .set(ResourceKind::CpuThreads, 32),
        maximum_reference_cache_bytes: 64 * 1024 * 1024,
        maximum_tool_output_bytes: 8 * 1024 * 1024,
        maximum_children: 16,
        tool_poll_interval: Duration::from_millis(10),
    };
    NodeAgentCore::new(
        config,
        health,
        mindclade_node_agent::telemetry::NodeMetrics::default(),
    )
    .expect("node agent core")
}

fn temporary_root(label: &str) -> PathBuf {
    let root = std::env::temp_dir().join(format!(
        "mindclade-node-agent-fi-{label}-{}-{:?}",
        std::process::id(),
        std::time::Instant::now(),
    ));
    std::fs::create_dir_all(&root).expect("temporary CAS root");
    root
}

/// Scenario `lease_expires_during_preprocessing`, invariant
/// `no_stale_fenced_commit`.
///
/// Two ways a stale holder could commit, and both must be closed:
/// admission must reject a ticket whose lease has already expired, and a holder
/// carrying a superseded fence must be unable to commit even if it is inside
/// the runtime.
#[tokio::test]
async fn lease_expiry_rejects_stale_commit() {
    let health = Arc::new(NodeHealth::new());
    let core = core(health.clone());

    // The lease expired at 5_000. `NodeAgentCore` reads the system clock, so
    // any wall clock after 1970 is far past it.
    let expired = ticket(7, 5_000);
    let rejected = core
        .execute(
            &expired,
            "preprocess",
            &[],
            1,
            1,
            &RevocationSnapshot::empty(1, 1, 1_000_000_000_000),
            &AcceptingVerifier,
            &UnreachableExecutor,
        )
        .await
        .expect_err("an expired lease must not be admitted");
    assert!(
        matches!(
            rejected.code(),
            Code::DeadlineExceeded | Code::InvalidArgument | Code::FailedPrecondition
        ),
        "{rejected:?}"
    );

    // Admission failure must not leak a stage slot, and must not trip the
    // accounting latch: a rejected ticket is a normal outcome, not corruption.
    assert_eq!(core.active_stage_count(), 0);
    let snapshot = health.snapshot();
    assert_eq!(snapshot.active_stages, 0);
    assert!(snapshot.accounting_healthy);
    assert!(snapshot.accepting);

    // A holder with a superseded fence cannot reach either commit point, even
    // though it holds a valid lease for the current one.
    let runtime = WorkerRuntime::new(mindclade_runtime_core::Budget::root(
        "fencing",
        ResourceVector::default(),
    ));
    runtime.start().expect("start");
    let current = ticket(8, 1_000_000_000_000);
    runtime
        .lease(
            &current,
            1_000,
            1,
            1,
            &RevocationSnapshot::empty(1, 1, 1_000_000_000_000),
            &AcceptingVerifier,
        )
        .expect("lease with the current fence");
    runtime.run().expect("run");
    let stale = FencingToken::new(7).expect("stale fence");
    assert!(
        runtime.begin_commit(stale).is_err(),
        "a superseded fence must not begin a commit"
    );
    assert!(
        runtime.complete(stale).is_err(),
        "a superseded fence must not complete a commit"
    );
    runtime
        .begin_commit(FencingToken::new(8).expect("current fence"))
        .expect("the current fence still commits");
}

/// Scenario `checkpoint_upload_interrupted`, invariant
/// `atomic_checkpoint_commit`.
///
/// A checkpoint is many blobs and one manifest, and the manifest is the commit
/// point. An upload interrupted partway has written some blobs and no manifest,
/// so what must hold is that the incomplete set cannot be published and that
/// nothing partial is loadable in the meantime.
#[tokio::test]
async fn checkpoint_interruption_preserves_atomicity() {
    let root = temporary_root("checkpoint");
    let store = Arc::new(LocalStore::new(&root).expect("local store"));
    let cas = ArtifactCas::new(store, Arc::new(SystemClock), CasConfig::default())
        .expect("content-addressed store");

    let first = b"checkpoint-shard-0".as_slice();
    let second = b"checkpoint-shard-1".as_slice();
    let first_digest = cas.put_blob(first).expect("first shard uploaded");
    let second_digest = hash_bytes(second);

    // The writer dies here: shard 0 is durable, shard 1 never arrived.
    assert!(cas.contains_blob(first_digest).expect("blob lookup"));
    assert!(!cas.contains_blob(second_digest).expect("blob lookup"));

    let artifact_id = id("artifact", 9);
    let manifest = ArtifactManifest::new(
        artifact_id.clone(),
        Name::new("checkpoint").expect("kind"),
        vec![
            BlobRef::new(
                Name::new("shard-0").expect("path"),
                first_digest,
                first.len() as u64,
                "application/octet-stream",
            )
            .expect("blob ref"),
            BlobRef::new(
                Name::new("shard-1").expect("path"),
                second_digest,
                second.len() as u64,
                "application/octet-stream",
            )
            .expect("blob ref"),
        ],
    )
    .expect("manifest");

    let refused = cas
        .publish_manifest(&manifest)
        .expect_err("a manifest referencing an unfinished upload must not publish");
    assert_eq!(refused.code(), Code::FailedPrecondition, "{refused:?}");

    // And nothing partial is visible to a reader in the meantime.
    assert!(
        cas.load_manifest(&artifact_id.to_string()).is_err(),
        "an interrupted checkpoint must not be loadable"
    );

    // The same manifest publishes once the interrupted upload is resumed, so
    // the refusal above is the missing blob rather than a manifest that can
    // never commit.
    cas.put_blob(second).expect("resumed shard uploaded");
    cas.publish_manifest(&manifest)
        .expect("a complete checkpoint commits");
    let loaded = cas
        .load_manifest(&artifact_id.to_string())
        .expect("committed checkpoint is loadable");
    assert_eq!(loaded.blobs.len(), 2);

    let _ = std::fs::remove_dir_all(root);
}
