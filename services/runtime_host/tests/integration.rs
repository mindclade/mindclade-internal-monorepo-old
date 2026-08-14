use mindclade_content_digest::hash_bytes;
use mindclade_faults::FaultResult;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::{
    FencingToken, ResourceKind, ResourceVector
};
use mindclade_runtime_host::{
    HostConfig, HostCore, HostHealth, ProcessHandle, ProcessLauncher, ProcessSpec
};
use mindclade_worker_protocol::{
    ArtifactGrant, DetachedSignature, ExecutionBudget, ExecutionTicket, ExecutionTicketClaims,
    RevocationSnapshot, RevocationSnapshotClaims, SignatureVerifier,
};
use mindclade_gpu_host::DeviceCapability;
use std::collections::{
    BTreeMap, BTreeSet
};
use std::sync::{
    Arc, Mutex
};

struct AcceptAll;

impl SignatureVerifier for AcceptAll {
    fn verify(&self, _: &[u8], _: &DetachedSignature) -> FaultResult<()> {
        Ok(())
    }
}

#[derive(Default)]
struct RecordingLauncher {
    next: Mutex<u32>
}

impl ProcessLauncher for RecordingLauncher {
    fn launch(&self, _: &ProcessSpec) -> FaultResult<ProcessHandle> {
        let mut next=self.next.lock().unwrap();
        *next+=1;
        Ok(ProcessHandle {
            pid: *next
        })
    }
    fn terminate(&self, _: ProcessHandle) -> FaultResult<()> {
        Ok(())
    }
    fn running(&self, _: ProcessHandle) -> FaultResult<bool> {
        Ok(true)
    }
}

fn id(kind: &str, suffix: &str) -> ResourceId {
    format!("{kind}_01890f2c7b7a70008{suffix}").parse().expect("id")
}

fn sig() -> DetachedSignature {
    DetachedSignature {
        algorithm: "test".into(), key_id: "test".into(), value: vec![1]
    }
}

fn resources() -> ResourceVector {
    ResourceVector::new().set(ResourceKind::CpuMillis, 8000).set(ResourceKind::ResidentMemoryBytes, 32<<30)
    .set(ResourceKind::PinnedMemoryBytes, 8<<30).set(ResourceKind::SharedMemoryBytes, 8<<30).set(ResourceKind::LocalDiskBytes,
    100<<30).set(ResourceKind::OpenFileDescriptors, 4096).set(ResourceKind::ObjectStoreRequests, 128).set(ResourceKind::QueuedRequests,
    128).set(ResourceKind::Processes, 16).set(ResourceKind::CpuThreads, 64).set(ResourceKind::GpuMemoryEstimateBytes,
    80<<30)
}

fn revocations() -> RevocationSnapshot {
    RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1, created_unix_millis: 100, expires_unix_millis: 10_000, revoked_grant_ids: BTreeSet::new(), revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(), revoked_bundle_digests: BTreeSet::new()
        }, signature: sig()
    }
}

#[test]
fn host_revalidates_ticket_and_reserves_node_resources() {
    let config=HostConfig {
        maximum_processes: 8, maximum_model_slots: 4, maximum_input_buffers: 16, maximum_control_payload_bytes: 256*1024,
        node_resources: resources()
    };
    let health=Arc::new(HostHealth::new());
    let host=HostCore::new(config, DeviceCapability {
        vendor: "nvidia".into(), architecture: "hopper".into(), total_memory_bytes: 80<<30
    }, Arc::new(RecordingLauncher::default()), health.clone()).expect("host");
    health.set_accepting(true);
    let ticket=ExecutionTicket {
        claims: ExecutionTicketClaims {
            ticket_id: id("ticket", "000000000000001"), issuer: "control-plane".into(), tenant_id: id("tenant", "000000000000002"),
            workspace_id: id("workspace", "000000000000003"), run_id: None, job_id: Some(id("job", "000000000000004")),
            stage_id: None, request_id: None, attempt: 1, fencing_token: FencingToken::new(1).unwrap(), model_bundle: None,
            engine_bundle: None, resolved_config_digest: hash_bytes(b"config"), reference_snapshot: None, artifacts: ArtifactGrant {
                readable_digests: BTreeSet::new(), writable_namespaces: BTreeSet::new(), maximum_read_bytes: 0, maximum_write_bytes: 0,
                allow_range_reads: false, allow_multipart_writes: false
            }, budget: ExecutionBudget {
                resources: ResourceVector::new().set(ResourceKind::CpuMillis, 1000).set(ResourceKind::ResidentMemoryBytes,
                1<<30).set(ResourceKind::OpenFileDescriptors, 64).set(ResourceKind::CpuThreads, 4), maximum_output_bytes: 1<<20
            }, execution_class: "batch".into(), accelerator_capability: "".into(), not_before_unix_millis: 100, deadline_unix_millis: 2_000,
            expires_unix_millis: 1_000, policy_epoch: 1, route_snapshot_version: 1, revocation_epoch: 1, idempotency_key: "test".into()
        }, signature: sig()
    };
    let session=host.begin_execution(&ticket, Vec::new(), 200, 1, 1, 1, &revocations(), &AcceptAll).expect("session");
    assert_eq!(format!("{:?}", session.state()), "Running");
    session.commit().expect("commit");
    assert_eq!(format!("{:?}", session.state()), "Completed");
}

#[test]
fn process_spec_is_bounded() {
    let spec=ProcessSpec {
        name: "worker".into(), executable: "/usr/bin/python3".into(), arguments: vec!["-m".into(), "mindclade.worker".into()],
        environment: BTreeMap::new()
    };
    spec.validate().expect("valid process spec");
}
