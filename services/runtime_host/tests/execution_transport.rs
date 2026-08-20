// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use hyper_util::rt::TokioIo;
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::FaultResult;
use mindclade_gpu_host::DeviceCapability;
use mindclade_protocols::runtime::v1::worker_control_client::WorkerControlClient;
use mindclade_protocols::runtime::v1::{
    AccessMode, ArtifactGrant, BufferDescriptor, CancelCommand, DetachedSignature as WireSignature,
    ExecutionBudget, ExecutionTicket, ExecutionTicketClaims, StartCommand, Transport,
    WorkerCommand, WorkerState, WorkerStatus, worker_command,
};
use mindclade_runtime_core::{ResourceKind, ResourceVector};
use mindclade_runtime_host::grpc::{WorkerControlService, serve_unix};
use mindclade_runtime_host::{
    HostAuthority, HostConfig, HostCore, HostHealth, ModelSpec, ProcessHandle, ProcessLauncher,
    ProcessSpec, StdProcessLauncher,
};
use mindclade_worker_protocol::{
    DetachedSignature, RevocationSnapshot, RevocationSnapshotClaims, SignatureVerifier,
};
use prost::Message;
use std::collections::{BTreeMap, BTreeSet};
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::str::FromStr;
use std::sync::Arc;
use std::sync::atomic::{AtomicU32, Ordering};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{UnixListener, UnixStream};
use tokio::sync::{mpsc, watch};
use tokio_stream::wrappers::ReceiverStream;
use tonic::transport::Endpoint;
use tower::service_fn;

#[derive(Debug)]
struct AcceptAll;

impl SignatureVerifier for AcceptAll {
    fn verify(&self, _payload: &[u8], _signature: &DetachedSignature) -> FaultResult<()> {
        Ok(())
    }
}

#[derive(Debug, Default)]
struct RecordingLauncher {
    next: AtomicU32,
}

impl ProcessLauncher for RecordingLauncher {
    fn launch(&self, _spec: &ProcessSpec) -> FaultResult<ProcessHandle> {
        Ok(ProcessHandle {
            pid: self.next.fetch_add(1, Ordering::AcqRel) + 1,
        })
    }

    fn terminate(&self, _handle: ProcessHandle) -> FaultResult<()> {
        Ok(())
    }

    fn running(&self, _handle: ProcessHandle) -> FaultResult<bool> {
        Ok(true)
    }
}

fn temporary_socket(name: &str) -> PathBuf {
    PathBuf::from(format!(
        "/tmp/mc-{name}-{}-{}.sock",
        std::process::id(),
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos(),
    ))
}

fn id(kind: &str, suffix: &str) -> String {
    format!("{kind}_01890f2c7b7a70008{suffix}")
}

fn now_millis() -> u64 {
    u64::try_from(
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_millis(),
    )
    .expect("time")
}

fn revocations(now: u64) -> RevocationSnapshot {
    RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1,
            created_unix_millis: now - 1,
            expires_unix_millis: now + 60_000,
            revoked_grant_ids: BTreeSet::new(),
            revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(),
            revoked_bundle_digests: BTreeSet::new(),
        },
        signature: DetachedSignature {
            algorithm: "test".to_owned(),
            key_id: "test".to_owned(),
            value: vec![1],
        },
    }
}

fn host_config() -> HostConfig {
    HostConfig {
        maximum_processes: 2,
        maximum_model_slots: 1,
        maximum_input_buffers: 16,
        maximum_control_payload_bytes: 1024 * 1024,
        node_resources: ResourceVector::new()
            .set(ResourceKind::CpuMillis, 10_000)
            .set(ResourceKind::ResidentMemoryBytes, 1024 * 1024 * 1024)
            .set(ResourceKind::PinnedMemoryBytes, 1024 * 1024)
            .set(ResourceKind::SharedMemoryBytes, 1024 * 1024)
            .set(ResourceKind::LocalDiskBytes, 1024 * 1024 * 1024)
            .set(ResourceKind::OpenFileDescriptors, 1024)
            .set(ResourceKind::Processes, 8)
            .set(ResourceKind::CpuThreads, 16)
            .set(ResourceKind::GpuMemoryEstimateBytes, 1024 * 1024 * 1024),
    }
}

fn ticket(now: u64, model: &str) -> ExecutionTicket {
    ExecutionTicket {
        claims: Some(ExecutionTicketClaims {
            ticket_id: id("ticket", "000000000000001"),
            issuer: "test-control-plane".to_owned(),
            tenant_id: id("tenant", "000000000000002"),
            workspace_id: id("workspace", "000000000000003"),
            run_id: String::new(),
            job_id: String::new(),
            stage_id: String::new(),
            request_id: id("request", "000000000000004"),
            attempt: 1,
            fencing_token: 7,
            model_bundle_digest: model.to_owned(),
            engine_bundle_digest: hash_bytes(b"engine").to_string(),
            resolved_config_digest: hash_bytes(b"config").to_string(),
            reference_snapshot_digest: String::new(),
            artifacts: Some(ArtifactGrant {
                readable_digests: Vec::new(),
                writable_namespaces: Vec::new(),
                maximum_read_bytes: 0,
                maximum_write_bytes: 0,
                allow_range_reads: false,
                allow_multipart_writes: false,
            }),
            budget: Some(ExecutionBudget {
                cpu_millis: 1000,
                resident_memory_bytes: 16 * 1024 * 1024,
                pinned_memory_bytes: 0,
                shared_memory_bytes: 0,
                local_disk_bytes: 0,
                open_file_descriptors: 32,
                object_store_requests: 0,
                queued_operations: 0,
                child_processes: 0,
                cpu_worker_threads: 2,
                gpu_memory_estimate_bytes: 0,
                checkpoint_staging_bytes: 0,
                telemetry_spool_bytes: 0,
                maximum_output_bytes: 1024,
            }),
            execution_class: "online".to_owned(),
            accelerator_capability: "test".to_owned(),
            not_before_unix_millis: now - 1,
            deadline_unix_millis: now + 30_000,
            expires_unix_millis: now + 20_000,
            policy_epoch: 1,
            route_snapshot_version: 1,
            revocation_epoch: 1,
            idempotency_key: "transport-test".to_owned(),
        }),
        signature: Some(WireSignature {
            algorithm: "test".to_owned(),
            key_id: "test".to_owned(),
            value: vec![1],
        }),
    }
}

async fn read_message(stream: &mut UnixStream) -> WorkerCommand {
    let length = stream.read_u32().await.expect("frame length");
    let mut bytes = vec![0_u8; usize::try_from(length).expect("length")];
    stream.read_exact(&mut bytes).await.expect("frame body");
    WorkerCommand::decode(bytes.as_slice()).expect("worker command")
}

async fn write_message(stream: &mut UnixStream, status: &WorkerStatus) {
    let bytes = status.encode_to_vec();
    stream
        .write_u32(u32::try_from(bytes.len()).expect("length"))
        .await
        .expect("frame length");
    stream.write_all(&bytes).await.expect("frame body");
}

async fn connect_client(path: &Path) -> WorkerControlClient<tonic::transport::Channel> {
    let deadline = tokio::time::Instant::now() + Duration::from_secs(15);
    loop {
        let socket_path = path.to_path_buf();
        match Endpoint::from_static("http://[::]:50051")
            .connect_with_connector(service_fn(move |_| {
                let path = socket_path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }))
            .await
        {
            Ok(channel) => return WorkerControlClient::new(channel),
            Err(_) if tokio::time::Instant::now() < deadline => {
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
            Err(error) => panic!("runtime-host channel did not become ready: {error}"),
        }
    }
}

fn bind_worker(path: &Path) -> UnixListener {
    let listener = UnixListener::bind(path).expect("worker listener");
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))
        .expect("worker socket permissions");
    listener
}

fn host_with_model(control_socket: PathBuf, model_digest: Digest, name: &str) -> Arc<HostCore> {
    let host = Arc::new(
        HostCore::new(
            host_config(),
            DeviceCapability {
                vendor: "test".to_owned(),
                architecture: "test".to_owned(),
                total_memory_bytes: 1024 * 1024 * 1024,
            },
            Arc::new(RecordingLauncher::default()),
            Arc::new(HostHealth::new()),
        )
        .expect("host"),
    );
    host.models()
        .load(ModelSpec {
            model_digest,
            minimum_gpu_memory_bytes: 1024,
            pinned_memory_bytes: 0,
            control_socket,
            process: ProcessSpec {
                name: name.to_owned(),
                executable: "/usr/bin/true".to_owned(),
                arguments: Vec::new(),
                environment: BTreeMap::new(),
            },
        })
        .expect("model load");
    host.resume_admission();
    host
}

async fn wait_for_socket(path: &Path) {
    tokio::time::timeout(Duration::from_secs(15), async {
        while !path.exists() {
            tokio::task::yield_now().await;
        }
    })
    .await
    .expect("host listener readiness");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn grpc_execution_reaches_model_worker_and_returns_terminal_status() {
    let now = now_millis();
    let host_socket = temporary_socket("host");
    let worker_socket = temporary_socket("worker");
    let worker_listener = bind_worker(&worker_socket);
    let model = hash_bytes(b"model");
    let host = host_with_model(worker_socket.clone(), model, "model-worker");
    let authority = Arc::new(
        HostAuthority::with_verifier(Arc::new(AcceptAll), revocations(now), now)
            .expect("authority"),
    );
    let service = WorkerControlService::new(host, authority);
    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let server_path = host_socket.clone();
    let server = tokio::spawn(async move { serve_unix(server_path, service, shutdown_rx).await });
    wait_for_socket(&host_socket).await;

    let worker_ticket = ticket(now, &model.to_string());
    let expected_ticket = worker_ticket
        .claims
        .as_ref()
        .expect("claims")
        .ticket_id
        .clone();
    let worker = tokio::spawn(async move {
        let (mut stream, _) = worker_listener.accept().await.expect("worker accept");
        let command = read_message(&mut stream).await;
        assert!(matches!(
            command.command,
            Some(worker_command::Command::Start(_))
        ));
        for (sequence, state) in [(1, WorkerState::Running), (2, WorkerState::Completed)] {
            write_message(
                &mut stream,
                &WorkerStatus {
                    sequence,
                    ticket_id: expected_ticket.clone(),
                    fencing_token: 7,
                    state: state as i32,
                    observed_unix_millis: now_millis(),
                    message: format!("{state:?}"),
                    outputs: Vec::new(),
                    diagnostic_artifact_digest: String::new(),
                },
            )
            .await;
        }
    });

    let mut client = connect_client(&host_socket).await;
    let (commands_tx, commands_rx) = mpsc::channel(4);
    commands_tx
        .send(WorkerCommand {
            sequence: 1,
            command: Some(worker_command::Command::Start(StartCommand {
                ticket: Some(worker_ticket),
                inputs: Vec::new(),
                operation: "forward".to_owned(),
            })),
        })
        .await
        .expect("start command");
    let mut statuses = client
        .execute(ReceiverStream::new(commands_rx))
        .await
        .expect("execute")
        .into_inner();
    let mut states = Vec::new();
    while let Some(status) = tokio::time::timeout(Duration::from_secs(2), statuses.message())
        .await
        .expect("status deadline")
        .expect("status stream")
    {
        states.push(WorkerState::try_from(status.state).expect("state"));
        if status.state == WorkerState::Completed as i32 {
            break;
        }
    }
    assert_eq!(states.last(), Some(&WorkerState::Completed));
    assert!(states.contains(&WorkerState::Running));
    worker.await.expect("worker task");
    drop(commands_tx);
    shutdown_tx.send(true).expect("shutdown");
    server.await.expect("server task").expect("server shutdown");
    let _ = std::fs::remove_file(worker_socket);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn grpc_cancellation_is_forwarded_and_waits_for_worker_acknowledgement() {
    let now = now_millis();
    let host_socket = temporary_socket("cancel-host");
    let worker_socket = temporary_socket("cancel-worker");
    let worker_listener = bind_worker(&worker_socket);
    let model = hash_bytes(b"cancel-model");
    let host = host_with_model(worker_socket.clone(), model, "cancel-model-worker");
    let authority = Arc::new(
        HostAuthority::with_verifier(Arc::new(AcceptAll), revocations(now), now)
            .expect("authority"),
    );
    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let server_path = host_socket.clone();
    let server = tokio::spawn(async move {
        serve_unix(
            server_path,
            WorkerControlService::new(host, authority),
            shutdown_rx,
        )
        .await
    });
    wait_for_socket(&host_socket).await;

    let worker_ticket = ticket(now, &model.to_string());
    let expected_ticket = worker_ticket
        .claims
        .as_ref()
        .expect("claims")
        .ticket_id
        .clone();
    let worker = tokio::spawn(async move {
        let (mut stream, _) = worker_listener.accept().await.expect("worker accept");
        let start = read_message(&mut stream).await;
        assert!(matches!(
            start.command,
            Some(worker_command::Command::Start(_))
        ));
        let cancel = read_message(&mut stream).await;
        assert!(matches!(
            cancel.command,
            Some(worker_command::Command::Cancel(_))
        ));
        write_message(
            &mut stream,
            &WorkerStatus {
                sequence: 1,
                ticket_id: expected_ticket,
                fencing_token: 7,
                state: WorkerState::Cancelled as i32,
                observed_unix_millis: now_millis(),
                message: "cancelled".to_owned(),
                outputs: Vec::new(),
                diagnostic_artifact_digest: String::new(),
            },
        )
        .await;
    });

    let mut client = connect_client(&host_socket).await;
    let (commands_tx, commands_rx) = mpsc::channel(4);
    commands_tx
        .send(WorkerCommand {
            sequence: 1,
            command: Some(worker_command::Command::Start(StartCommand {
                ticket: Some(worker_ticket),
                inputs: Vec::new(),
                operation: "forward".to_owned(),
            })),
        })
        .await
        .expect("start command");
    let mut statuses = client
        .execute(ReceiverStream::new(commands_rx))
        .await
        .expect("execute")
        .into_inner();
    let admitted = statuses
        .message()
        .await
        .expect("status stream")
        .expect("admitted status");
    assert_eq!(admitted.state, WorkerState::Running as i32);
    commands_tx
        .send(WorkerCommand {
            sequence: 2,
            command: Some(worker_command::Command::Cancel(CancelCommand {
                reason: "client cancelled".to_owned(),
                deadline_unix_millis: now_millis() + 5_000,
            })),
        })
        .await
        .expect("cancel command");
    let terminal = tokio::time::timeout(Duration::from_secs(2), statuses.message())
        .await
        .expect("cancellation deadline")
        .expect("status stream")
        .expect("terminal status");
    assert_eq!(terminal.state, WorkerState::Cancelled as i32);
    worker.await.expect("worker task");
    drop(commands_tx);
    shutdown_tx.send(true).expect("shutdown");
    server.await.expect("server task").expect("server shutdown");
    let _ = std::fs::remove_file(worker_socket);
}

fn required_bazel_data(name: &str) -> Option<PathBuf> {
    let relative = std::env::var(name).ok()?;
    Some(
        std::env::current_dir()
            .expect("test working directory")
            .join(relative),
    )
}

fn bundle_digest(manifest: &str) -> String {
    let marker = "\"digest\": \"";
    let start = manifest.find(marker).expect("manifest digest") + marker.len();
    let end = manifest[start..].find('"').expect("manifest digest end") + start;
    manifest[start..end].to_owned()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
// This qualification test intentionally keeps the bundle, process, transport, and cleanup
// sequence together so a reader can audit the complete cross-language boundary in execution order.
#[allow(clippy::too_many_lines)]
async fn rust_host_executes_the_real_python_reference_worker() {
    let Some(worker_executable) = required_bazel_data("MINDCLADE_TEST_MODEL_WORKER") else {
        eprintln!("Bazel-only Python worker integration test skipped under Cargo");
        return;
    };
    let manifest_source =
        required_bazel_data("MINDCLADE_TEST_MODEL_MANIFEST").expect("Bazel model manifest path");
    let config_source =
        required_bazel_data("MINDCLADE_TEST_MODEL_CONFIG").expect("Bazel model config path");
    let weights_source =
        required_bazel_data("MINDCLADE_TEST_MODEL_WEIGHTS").expect("Bazel model weights path");

    let test_root = std::fs::canonicalize("/tmp")
        .expect("canonical temporary root")
        .join(format!(
            "mc-python-worker-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .expect("clock")
                .as_nanos(),
        ));
    let bundle_root = test_root.join("bundle");
    let input_root = test_root.join("inputs");
    let output_root = test_root.join("outputs");
    for directory in [&test_root, &bundle_root, &input_root, &output_root] {
        std::fs::create_dir(directory).expect("test directory");
        std::fs::set_permissions(directory, std::fs::Permissions::from_mode(0o700))
            .expect("test directory permissions");
    }
    std::fs::copy(&manifest_source, bundle_root.join("manifest.json")).expect("copy manifest");
    std::fs::copy(&config_source, bundle_root.join("config.json")).expect("copy model config");
    std::fs::copy(&weights_source, bundle_root.join("model.safetensors")).expect("copy weights");
    let manifest = std::fs::read_to_string(bundle_root.join("manifest.json")).expect("manifest");
    let model_text = bundle_digest(&manifest);
    let model = Digest::from_str(&model_text).expect("model digest");

    let worker_socket = test_root.join("worker.sock");
    let worker_config = test_root.join("worker.json");
    std::fs::write(
        &worker_config,
        format!(
            concat!(
                "{{",
                "\"schema_version\":1,",
                "\"model_bundle_root\":\"{}\",",
                "\"model_bundle_digest\":\"{}\",",
                "\"output_root\":\"{}\",",
                "\"allowed_input_roots\":[\"{}\"],",
                "\"device\":\"cpu\",",
                "\"maximum_pending_requests\":8,",
                "\"maximum_concurrent_executions\":2,",
                "\"maximum_input_bytes\":1048576,",
                "\"maximum_output_bytes\":1048576,",
                "\"io_timeout_millis\":5000,",
                "\"cancellation_grace_millis\":2000,",
                "\"reference_chunk_elements\":64,",
                "\"reference_iterations\":1",
                "}}"
            ),
            bundle_root.display(),
            model_text,
            output_root.display(),
            input_root.display(),
        ),
    )
    .expect("worker config");

    let input_bytes = [1.0_f32.to_le_bytes(), (-1.0_f32).to_le_bytes()].concat();
    let input_path = input_root.join("request.f32");
    std::fs::write(&input_path, &input_bytes).expect("input data");
    let worker_name = format!("python-model-worker-{}", std::process::id());
    let host = Arc::new(
        HostCore::new(
            host_config(),
            DeviceCapability {
                vendor: "test".to_owned(),
                architecture: "test".to_owned(),
                total_memory_bytes: 1024 * 1024 * 1024,
            },
            Arc::new(StdProcessLauncher::default()),
            Arc::new(HostHealth::new()),
        )
        .expect("host"),
    );
    host.models()
        .load(ModelSpec {
            model_digest: model,
            minimum_gpu_memory_bytes: 1024,
            pinned_memory_bytes: 0,
            control_socket: worker_socket.clone(),
            process: ProcessSpec {
                name: worker_name,
                executable: worker_executable.to_string_lossy().into_owned(),
                arguments: Vec::new(),
                environment: BTreeMap::from([
                    (
                        "MINDCLADE_MODEL_WORKER_CONFIG".to_owned(),
                        worker_config.to_string_lossy().into_owned(),
                    ),
                    (
                        "MINDCLADE_MODEL_WORKER_SOCKET".to_owned(),
                        worker_socket.to_string_lossy().into_owned(),
                    ),
                ]),
            },
        })
        .expect("load Python model worker");
    wait_for_socket(&worker_socket).await;
    host.resume_admission();

    let now = now_millis();
    let authority = Arc::new(
        HostAuthority::with_verifier(Arc::new(AcceptAll), revocations(now), now)
            .expect("authority"),
    );
    let host_socket = test_root.join("host.sock");
    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let server_path = host_socket.clone();
    let service_host = host.clone();
    let server = tokio::spawn(async move {
        serve_unix(
            server_path,
            WorkerControlService::new(service_host, authority),
            shutdown_rx,
        )
        .await
    });
    wait_for_socket(&host_socket).await;

    let mut worker_ticket = ticket(now, &model_text);
    let claims = worker_ticket.claims.as_mut().expect("claims");
    claims
        .artifacts
        .as_mut()
        .expect("artifacts")
        .maximum_read_bytes = u64::try_from(input_bytes.len()).expect("input length");
    claims.budget.as_mut().expect("budget").maximum_output_bytes =
        u64::try_from(input_bytes.len()).expect("output length");
    let descriptor = BufferDescriptor {
        segment_id: "reference-input".to_owned(),
        generation: 1,
        offset_bytes: 0,
        length_bytes: u64::try_from(input_bytes.len()).expect("input length"),
        element_type: "f32".to_owned(),
        shape: vec![2],
        content_digest: hash_bytes(&input_bytes).to_string(),
        owner_process: "runtime-host-test".to_owned(),
        lease_expires_unix_millis: now + 20_000,
        access_mode: AccessMode::ReadOnly as i32,
        transport: Transport::LocalFile as i32,
        locator: input_path.to_string_lossy().into_owned(),
    };
    let mut client = connect_client(&host_socket).await;
    let (commands_tx, commands_rx) = mpsc::channel(4);
    commands_tx
        .send(WorkerCommand {
            sequence: 1,
            command: Some(worker_command::Command::Start(StartCommand {
                ticket: Some(worker_ticket),
                inputs: vec![descriptor],
                operation: "reference.affine.v1".to_owned(),
            })),
        })
        .await
        .expect("start command");
    let mut statuses = client
        .execute(ReceiverStream::new(commands_rx))
        .await
        .expect("execute")
        .into_inner();
    let mut completed = None;
    while let Some(status) = tokio::time::timeout(Duration::from_secs(10), statuses.message())
        .await
        .expect("Python worker status deadline")
        .expect("status stream")
    {
        if status.state == WorkerState::Completed as i32 {
            completed = Some(status);
            break;
        }
    }
    let completed = completed.expect("completed Python worker status");
    assert_eq!(completed.outputs.len(), 1);
    let output = std::fs::read(&completed.outputs[0].locator).expect("worker output");
    assert_eq!(
        output,
        [2.5_f32.to_le_bytes(), (-1.5_f32).to_le_bytes()].concat()
    );

    drop(commands_tx);
    shutdown_tx.send(true).expect("shutdown");
    server.await.expect("server task").expect("server shutdown");
    host.models().unload(&model).expect("unload model worker");
    std::fs::remove_dir_all(&test_root).expect("remove isolated test root");
}
