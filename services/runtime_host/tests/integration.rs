// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, FaultResult};
use mindclade_gpu_host::DeviceCapability;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector};
use mindclade_runtime_host::{
    HostConfig, HostCore, HostHealth, ProcessHandle, ProcessLauncher, ProcessSpec,
};
use mindclade_worker_protocol::{
    ArtifactGrant, DetachedSignature, ExecutionBudget, ExecutionTicket, ExecutionTicketClaims,
    RevocationSnapshot, RevocationSnapshotClaims, SignatureVerifier,
};
use std::collections::{BTreeMap, BTreeSet};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

struct AcceptAll;

impl SignatureVerifier for AcceptAll {
    fn verify(&self, _: &[u8], _: &DetachedSignature) -> FaultResult<()> {
        Ok(())
    }
}

struct RecordingLauncher {
    next: Mutex<u32>,
    running: AtomicBool,
}

impl Default for RecordingLauncher {
    fn default() -> Self {
        Self {
            next: Mutex::new(0),
            running: AtomicBool::new(true),
        }
    }
}

impl ProcessLauncher for RecordingLauncher {
    fn launch(&self, _: &ProcessSpec) -> FaultResult<ProcessHandle> {
        let mut next = self.next.lock().unwrap();
        *next += 1;
        Ok(ProcessHandle { pid: *next })
    }
    fn terminate(&self, _: ProcessHandle) -> FaultResult<()> {
        self.running.store(false, Ordering::Release);
        Ok(())
    }
    fn running(&self, _: ProcessHandle) -> FaultResult<bool> {
        Ok(self.running.load(Ordering::Acquire))
    }
}

fn id(kind: &str, suffix: &str) -> ResourceId {
    format!("{kind}_01890f2c7b7a70008{suffix}")
        .parse()
        .expect("id")
}

fn sig() -> DetachedSignature {
    DetachedSignature {
        algorithm: "test".into(),
        key_id: "test".into(),
        value: vec![1],
    }
}

fn resources() -> ResourceVector {
    ResourceVector::new()
        .set(ResourceKind::CpuMillis, 8000)
        .set(ResourceKind::ResidentMemoryBytes, 32 << 30)
        .set(ResourceKind::PinnedMemoryBytes, 8 << 30)
        .set(ResourceKind::SharedMemoryBytes, 8 << 30)
        .set(ResourceKind::LocalDiskBytes, 100 << 30)
        .set(ResourceKind::OpenFileDescriptors, 4096)
        .set(ResourceKind::ObjectStoreRequests, 128)
        .set(ResourceKind::QueuedRequests, 128)
        .set(ResourceKind::Processes, 16)
        .set(ResourceKind::CpuThreads, 64)
        .set(ResourceKind::GpuMemoryEstimateBytes, 80 << 30)
}

fn host_config() -> HostConfig {
    HostConfig {
        maximum_processes: 8,
        maximum_model_slots: 4,
        maximum_input_buffers: 16,
        maximum_control_payload_bytes: 256 * 1024,
        node_resources: resources(),
    }
}

fn execution_ticket() -> ExecutionTicket {
    ExecutionTicket {
        claims: ExecutionTicketClaims {
            ticket_id: id("ticket", "000000000000001"),
            issuer: "control-plane".into(),
            tenant_id: id("tenant", "000000000000002"),
            workspace_id: id("workspace", "000000000000003"),
            run_id: None,
            job_id: Some(id("job", "000000000000004")),
            stage_id: None,
            request_id: None,
            attempt: 1,
            fencing_token: FencingToken::new(1).unwrap(),
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
                    .set(ResourceKind::ResidentMemoryBytes, 1 << 30)
                    .set(ResourceKind::OpenFileDescriptors, 64)
                    .set(ResourceKind::CpuThreads, 4),
                maximum_output_bytes: 1 << 20,
            },
            execution_class: "batch".into(),
            accelerator_capability: String::new(),
            not_before_unix_millis: 100,
            deadline_unix_millis: 2_000,
            expires_unix_millis: 1_000,
            policy_epoch: 1,
            route_snapshot_version: 1,
            revocation_epoch: 1,
            idempotency_key: "test".into(),
        },
        signature: sig(),
    }
}

fn revocations() -> RevocationSnapshot {
    RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1,
            created_unix_millis: 100,
            expires_unix_millis: 10_000,
            revoked_grant_ids: BTreeSet::new(),
            revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(),
            revoked_bundle_digests: BTreeSet::new(),
        },
        signature: sig(),
    }
}

#[test]
fn host_revalidates_ticket_and_reserves_node_resources() {
    let health = Arc::new(HostHealth::new());
    let host = HostCore::new(
        host_config(),
        DeviceCapability {
            vendor: "nvidia".into(),
            architecture: "hopper".into(),
            total_memory_bytes: 80 << 30,
        },
        Arc::new(RecordingLauncher::default()),
        health.clone(),
    )
    .expect("host");
    host.resume_admission();
    let ticket = execution_ticket();
    let session = host
        .begin_execution(
            &ticket,
            Vec::new(),
            200,
            1,
            1,
            1,
            &revocations(),
            &AcceptAll,
        )
        .expect("session");
    assert_eq!(format!("{:?}", session.state()), "Running");
    session.commit().expect("commit");
    assert_eq!(format!("{:?}", session.state()), "Completed");
}

#[test]
fn host_drain_closes_execution_admission() {
    let health = Arc::new(HostHealth::new());
    let host = HostCore::new(
        host_config(),
        DeviceCapability {
            vendor: "nvidia".into(),
            architecture: "hopper".into(),
            total_memory_bytes: 80 << 30,
        },
        Arc::new(RecordingLauncher::default()),
        health,
    )
    .expect("host");
    host.resume_admission();
    host.begin_drain();

    let fault = host
        .begin_execution(
            &execution_ticket(),
            Vec::new(),
            200,
            1,
            1,
            1,
            &revocations(),
            &AcceptAll,
        )
        .expect_err("draining host must reject new work");
    assert_eq!(fault.code(), Code::Unavailable);
}

#[test]
fn process_spec_is_bounded() {
    let spec = ProcessSpec {
        name: "worker".into(),
        executable: "/usr/bin/python3".into(),
        arguments: vec!["-m".into(), "mindclade.worker".into()],
        environment: BTreeMap::new(),
    };
    spec.validate().expect("valid process spec");
}

#[test]
fn supervisor_removes_workers_that_have_exited() {
    use mindclade_runtime_host::ProcessSupervisor;

    let launcher = Arc::new(RecordingLauncher::default());
    let supervisor = ProcessSupervisor::new(launcher.clone(), 1).expect("supervisor");
    let spec = ProcessSpec {
        name: "worker".into(),
        executable: "/usr/bin/python3".into(),
        arguments: Vec::new(),
        environment: BTreeMap::new(),
    };
    supervisor.launch(&spec).expect("launch");
    assert_eq!(supervisor.active(), 1);
    launcher.running.store(false, Ordering::Release);
    assert!(!supervisor.running("worker").expect("liveness check"));
    assert_eq!(supervisor.active(), 0);
}
