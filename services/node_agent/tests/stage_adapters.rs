// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_checkpoint_io::StagingBudget;
use mindclade_content_digest::hash_bytes;
use mindclade_identifiers::ResourceId;
use mindclade_ipc_os::{BulkBackend, BulkBufferBroker};
use mindclade_node_agent::{
    CHECKPOINT_COPY_OPERATION, CheckpointCopyExecutor, CheckpointTransfer,
    DATA_STREAM_FETCH_OPERATION, DataStreamFetchExecutor, NodeAgentConfig, NodeAgentCore,
    NodeHealth, ProviderSource, StreamWorker,
};
use mindclade_object_store::adapters::arrow::ArrowProvider;
use mindclade_object_store::{ClientConfig, Namespace, ObjectPath};
use mindclade_runtime_core::{FencingToken, ManualClock, Policy, ResourceKind, ResourceVector};
use mindclade_worker_protocol::{
    ArtifactGrant, BufferAccess, BufferDescriptor, BufferTransport, DetachedSignature,
    ExecutionBudget, ExecutionTicket, ExecutionTicketClaims, RevocationSnapshot, SignatureVerifier,
};
use std::collections::BTreeSet;
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime};

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

fn ticket(readable: BTreeSet<mindclade_content_digest::Digest>) -> ExecutionTicket {
    let resources = ResourceVector::new()
        .set(ResourceKind::CpuMillis, 10_000)
        .set(ResourceKind::ResidentMemoryBytes, 16 * 1024 * 1024)
        .set(ResourceKind::SharedMemoryBytes, 16 * 1024 * 1024)
        .set(ResourceKind::CheckpointStagingBytes, 16 * 1024 * 1024)
        .set(ResourceKind::OpenFileDescriptors, 64)
        .set(ResourceKind::ObjectStoreRequests, 64)
        .set(ResourceKind::QueuedRequests, 64)
        .set(ResourceKind::Processes, 4)
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
            fencing_token: FencingToken::new(7).expect("fence"),
            model_bundle: None,
            engine_bundle: None,
            resolved_config_digest: hash_bytes(b"config"),
            reference_snapshot: None,
            artifacts: ArtifactGrant {
                readable_digests: readable,
                writable_namespaces: BTreeSet::from([
                    "seed".to_owned(),
                    "checkpoint-output".to_owned(),
                ]),
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
            deadline_unix_millis: 10_000,
            expires_unix_millis: 9_000,
            policy_epoch: 1,
            route_snapshot_version: 1,
            revocation_epoch: 1,
            idempotency_key: "stage-adapter-test".into(),
        },
        signature: DetachedSignature {
            algorithm: "test-signature".into(),
            key_id: "test-key".into(),
            value: vec![1; 32],
        },
    }
}

fn config() -> NodeAgentConfig {
    NodeAgentConfig {
        node_resources: ResourceVector::new()
            .set(ResourceKind::CpuMillis, 100_000)
            .set(ResourceKind::ResidentMemoryBytes, 128 * 1024 * 1024)
            .set(ResourceKind::SharedMemoryBytes, 128 * 1024 * 1024)
            .set(ResourceKind::CheckpointStagingBytes, 128 * 1024 * 1024)
            .set(ResourceKind::LocalDiskBytes, 128 * 1024 * 1024)
            .set(ResourceKind::OpenFileDescriptors, 1_024)
            .set(ResourceKind::ObjectStoreRequests, 1_024)
            .set(ResourceKind::QueuedRequests, 1_024)
            .set(ResourceKind::Processes, 32)
            .set(ResourceKind::CpuThreads, 32),
        maximum_reference_cache_bytes: 64 * 1024 * 1024,
        maximum_tool_output_bytes: 8 * 1024 * 1024,
        maximum_children: 16,
        tool_poll_interval: Duration::from_millis(10),
    }
}

fn source() -> ProviderSource {
    let namespace = Namespace::new(ObjectPath::new("test/node-agent").expect("namespace"));
    let provider = ArrowProvider::memory(namespace, ClientConfig::default()).expect("provider");
    ProviderSource::new(provider)
}

fn artifact_input(digest: mindclade_content_digest::Digest, length: u64) -> BufferDescriptor {
    BufferDescriptor {
        segment_id: format!("artifact:{digest}"),
        generation: 1,
        range: ByteRange::new(0, length).expect("range"),
        element_type: "bytes".into(),
        shape: vec![length],
        digest,
        owner_process: "test".into(),
        lease_expires_unix_millis: 9_000,
        access: BufferAccess::ReadOnly,
        transport: BufferTransport::Artifact,
        locator: format!("artifact:{digest}"),
    }
}

fn core() -> NodeAgentCore {
    let clock = ManualClock::new(
        SystemTime::UNIX_EPOCH + Duration::from_millis(100),
        Instant::now(),
    );
    NodeAgentCore::with_clock(
        config(),
        Arc::new(NodeHealth::new()),
        Default::default(),
        Arc::new(clock),
    )
    .expect("node agent")
}

#[tokio::test]
async fn checkpoint_copy_preserves_digest_through_ticketed_stage() {
    let bytes = bytes::Bytes::from_static(b"checkpoint-component");
    let digest = hash_bytes(&bytes);
    let ticket = ticket(BTreeSet::from([digest]));
    let source = source();
    source
        .publish_artifact(&ticket, "seed", bytes.clone(), 100)
        .await
        .expect("seed artifact");

    let executor = CheckpointCopyExecutor::new(
        CheckpointTransfer::new(StagingBudget::new(ByteSize::new(1024))),
        source,
        "checkpoint-output",
        "node-agent-checkpoint",
    )
    .expect("executor");
    let input = artifact_input(digest, u64::try_from(bytes.len()).expect("length"));
    let result = core()
        .execute(
            &ticket,
            CHECKPOINT_COPY_OPERATION,
            &[input],
            1,
            1,
            &RevocationSnapshot::empty(1, 1, 8_000),
            &AcceptingVerifier,
            &executor,
        )
        .await
        .expect("checkpoint stage");
    assert_eq!(result.outputs.len(), 1);
    assert_eq!(result.outputs[0].digest, digest);
    assert_eq!(result.outputs[0].transport, BufferTransport::Artifact);
}

#[tokio::test]
async fn data_stream_fetch_materializes_verified_bulk_buffer() {
    let bytes = bytes::Bytes::from_static(b"training-shard");
    let digest = hash_bytes(&bytes);
    let ticket = ticket(BTreeSet::from([digest]));
    let source = source();
    source
        .publish_artifact(&ticket, "seed", bytes.clone(), 100)
        .await
        .expect("seed artifact");

    let root = std::env::temp_dir().join(format!("mindclade-node-stream-{}", std::process::id()));
    let backend = BulkBackend::local_file(&root).expect("backend");
    let broker = Arc::new(BulkBufferBroker::with_backend(backend, 8, 1024).expect("broker"));
    let executor = DataStreamFetchExecutor::new(
        StreamWorker {
            maximum_shard_bytes: ByteSize::new(1024),
            prefetch_depth: 2,
            retry_policy: Policy {
                max_attempts: 1,
                initial_delay: Duration::from_millis(1),
                maximum_delay: Duration::from_millis(1),
                multiplier_milli: 1_000,
                jitter_permyriad: 0,
            },
        },
        source,
        broker.clone(),
        "node-agent-data-stream",
    )
    .expect("executor");
    let input = artifact_input(digest, u64::try_from(bytes.len()).expect("length"));
    let result = core()
        .execute(
            &ticket,
            DATA_STREAM_FETCH_OPERATION,
            &[input],
            1,
            1,
            &RevocationSnapshot::empty(1, 1, 8_000),
            &AcceptingVerifier,
            &executor,
        )
        .await
        .expect("stream stage");
    assert_eq!(result.outputs.len(), 1);
    let descriptor = &result.outputs[0];
    assert_eq!(descriptor.digest, digest);
    assert_eq!(
        broker
            .read_verified(&descriptor.segment_id, 1024, 100)
            .expect("verified bulk read"),
        bytes.as_ref()
    );
    assert!(executor.release(&descriptor.segment_id));
    assert_eq!(broker.active(), 0);
    let _ = std::fs::remove_dir_all(root);
}
