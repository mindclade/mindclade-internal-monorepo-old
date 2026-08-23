// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Fail-closed runtime-host process bootstrap.
//!
//! The host starts only after immutable revocation authority, local resource
//! limits, GPU capability, and optional preloaded model-worker configuration
//! have been validated. Go remains the issuer of tickets and global policy;
//! this module owns only node-local execution state.

use crate::async_ipc::{self, AsyncControlSession, AsyncControlSessionFactory};
use crate::config;
use crate::grpc::{self, WorkerControlService};
use crate::protocol;
use crate::{
    HostAuthority, HostComponent, HostConfig, HostCore, HostHealth, ModelSpec, ProcessSpec,
    StdProcessLauncher,
};
use mindclade_config::{EnvSource, Snapshot};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_gpu_host::DeviceCapability;
use mindclade_protocols::runtime::v1 as wire;
use mindclade_runtime_core::{ResourceKind, ResourceVector};
use mindclade_servicekit::{Service, signals};
use mindclade_serving_runtime::host::HostInvocation;
use mindclade_worker_protocol::Digest;
use mindclade_worker_protocol::{Ed25519VerificationKey, WorkerState, WorkerStatus};
use prost::Message;
use std::collections::BTreeMap;
use std::future::Future;
use std::io::Read;
use std::path::PathBuf;
use std::pin::Pin;
use std::str::FromStr;
use std::sync::Arc;
use std::time::UNIX_EPOCH;
use tokio::sync::watch;

const MAX_POLICY_FILE_BYTES: u64 = 16 * 1024 * 1024;
const MAX_SOCKET_PATH_BYTES: usize = 100;
const MAX_KEY_ID_BYTES: usize = 256;

#[derive(Clone, Debug)]
pub struct BootstrapConfig {
    pub socket_path: PathBuf,
    pub grpc_socket_path: PathBuf,
    pub key_id: String,
    pub public_key: [u8; 32],
    pub key_not_before_unix_millis: u64,
    pub key_not_after_unix_millis: u64,
    pub revocation_snapshot_path: PathBuf,
    pub minimum_policy_epoch: u64,
    pub minimum_route_version: u64,
    pub minimum_revocation_epoch: u64,
    pub host: HostConfig,
    pub gpu: DeviceCapability,
    pub preloaded_model: Option<ModelSpec>,
}

impl BootstrapConfig {
    /// Resolves the bootstrap configuration from the process environment.
    pub fn from_env() -> FaultResult<Self> {
        Self::resolve(&EnvSource::process())
    }

    /// Resolves from an explicit variable table.
    ///
    /// The composition root uses [`BootstrapConfig::from_env`]. This exists so
    /// `tests/settings.rs` can pin the environment contract without
    /// `std::env::set_var`, which edition 2024 gates behind an audited block
    /// and which races every other test sharing the process.
    pub fn from_variables(variables: BTreeMap<String, String>) -> FaultResult<Self> {
        Self::resolve(&EnvSource::from_table(variables))
    }

    /// Order here is the pre-migration order, deliberately. When several
    /// settings are wrong at once the operator sees the same one reported first
    /// as before, and a startup-failure runbook keyed to that stays correct.
    fn resolve(lookup: &EnvSource) -> FaultResult<Self> {
        let settings = config::bind(lookup.clone());
        let snapshot = config::catalog()?.load(&[&settings])?;

        let socket_path = bounded_socket_path(
            &snapshot,
            "host.socket",
            "runtime-host socket path exceeds platform bound",
        )?;
        let grpc_socket_path = bounded_socket_path(
            &snapshot,
            "host.grpc.socket",
            "runtime-host gRPC socket path is invalid",
        )?;
        if grpc_socket_path == socket_path {
            return Err(Fault::invalid_argument(
                "runtime-host gRPC socket path is invalid",
            ));
        }
        let key_id = snapshot.string("key.id")?;
        if key_id.len() > MAX_KEY_ID_BYTES {
            return Err(Fault::invalid_argument("runtime key id exceeds bound"));
        }
        let public_key = decode_32_byte_hex(snapshot.raw("key.public.hex")?)?;
        let key_not_before_unix_millis = snapshot.u64("key.not.before.ms")?;
        let key_not_after_unix_millis = snapshot.u64("key.not.after.ms")?;
        if key_not_before_unix_millis >= key_not_after_unix_millis {
            return Err(Fault::invalid_argument(
                "runtime verification-key validity window is invalid",
            ));
        }
        let revocation_snapshot_path = snapshot.absolute_path("revocation.snapshot.file")?;
        let minimum_policy_epoch = snapshot.u64_positive("min.policy.epoch")?;
        let minimum_route_version = snapshot.u64_positive("min.route.version")?;
        let minimum_revocation_epoch = snapshot.u64_positive("min.revocation.epoch")?;

        let host = HostConfig {
            maximum_processes: snapshot.u32_positive("max.processes")?,
            maximum_model_slots: snapshot.u32_positive("max.model.slots")?,
            maximum_input_buffers: snapshot.usize_positive("max.input.buffers")?,
            maximum_control_payload_bytes: snapshot.u64_positive("max.control.bytes")?,
            node_resources: node_resources(&snapshot)?,
        };
        host.validate()?;

        let gpu = DeviceCapability {
            vendor: snapshot.string("gpu.vendor")?,
            architecture: snapshot.string("gpu.arch")?,
            total_memory_bytes: snapshot.u64_positive("gpu.memory.bytes")?,
        };
        gpu.validate()?;

        let preloaded_model = model_spec(lookup, &snapshot)?;
        Ok(Self {
            socket_path,
            grpc_socket_path,
            key_id,
            public_key,
            key_not_before_unix_millis,
            key_not_after_unix_millis,
            revocation_snapshot_path,
            minimum_policy_epoch,
            minimum_route_version,
            minimum_revocation_epoch,
            host,
            gpu,
            preloaded_model,
        })
    }
}

/// An absolute path that also fits the platform's `sun_path` limit.
fn bounded_socket_path(
    snapshot: &Snapshot,
    key: &str,
    message: &'static str,
) -> FaultResult<PathBuf> {
    let path = snapshot.absolute_path(key)?;
    if path.as_os_str().as_encoded_bytes().len() > MAX_SOCKET_PATH_BYTES {
        return Err(Fault::invalid_argument(message));
    }
    Ok(path)
}

pub async fn run(config: BootstrapConfig) -> FaultResult<()> {
    let now = unix_millis()?;
    let revocations = protocol::revocation_snapshot(read_message::<wire::RevocationSnapshot>(
        &config.revocation_snapshot_path,
    )?)?;
    if revocations.claims.epoch < config.minimum_revocation_epoch {
        return Err(Fault::new(
            Code::PermissionDenied,
            "runtime-host bootstrap revocation snapshot is below configured floor",
        ));
    }
    let authority = Arc::new(HostAuthority::from_ed25519_keys(
        [Ed25519VerificationKey {
            key_id: config.key_id,
            public_key: config.public_key,
            not_before_unix_millis: config.key_not_before_unix_millis,
            not_after_unix_millis: config.key_not_after_unix_millis,
            disabled: false,
        }],
        revocations,
        now,
    )?);
    authority.raise_policy_floor(
        config.minimum_policy_epoch,
        config.minimum_route_version,
        config.minimum_revocation_epoch,
    )?;

    let health = Arc::new(HostHealth::new());
    let core = Arc::new(HostCore::new(
        config.host,
        config.gpu,
        Arc::new(StdProcessLauncher::default()),
        health.clone(),
    )?);
    if let Some(model) = config.preloaded_model {
        core.models().load(model)?;
    }

    // servicekit owns start order, drain, and stop. Admission opens in the
    // component's start hook so it cannot precede the lifecycle.
    let mut service = Service::new();
    service.register(Box::new(HostComponent::new(core.clone(), health.clone())))?;
    service.start()?;

    let factory: Arc<dyn AsyncControlSessionFactory> = Arc::new(ControlFactory {
        host: core.clone(),
        authority: authority.clone(),
    });
    let grpc_service = WorkerControlService::new(core.clone(), authority);
    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let drain_core = core.clone();
    let signal = tokio::spawn(async move {
        signals::termination_requested().await;
        // Drain at signal time rather than waiting for the serve loop to
        // unwind, so admission closes the moment termination is requested.
        // Component::drain is required to be idempotent, so the reverse-order
        // pass below repeating this is harmless.
        drain_core.begin_drain();
        let _ = shutdown_tx.send(true);
    });

    let serve_result = tokio::try_join!(
        async_ipc::serve_unix_sessions(config.socket_path, factory, shutdown_rx.clone()),
        grpc::serve_unix(config.grpc_socket_path, grpc_service, shutdown_rx),
    )
    .map(|_| ());
    signal.abort();

    // Drain and stop run in reverse registration order here. The serve error, if
    // any, outranks a shutdown fault: it is the reason the process is ending.
    let shutdown_result = service.stop();
    serve_result.and(shutdown_result)
}

struct ControlFactory {
    host: Arc<HostCore>,
    authority: Arc<HostAuthority>,
}

impl AsyncControlSessionFactory for ControlFactory {
    fn open(&self) -> FaultResult<Box<dyn AsyncControlSession>> {
        Ok(Box::new(ControlSession::new(
            self.host.clone(),
            self.authority.clone(),
        )))
    }
}

pub(crate) struct ControlSession {
    host: Arc<HostCore>,
    authority: Arc<HostAuthority>,
    active: Option<crate::ExecutionSession>,
    last_command_sequence: u64,
    next_status_sequence: u64,
    last_worker_status_sequence: u64,
}

impl core::fmt::Debug for ControlSession {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("ControlSession")
            .field("has_active_execution", &self.active.is_some())
            .field("last_command_sequence", &self.last_command_sequence)
            .field("next_status_sequence", &self.next_status_sequence)
            .finish_non_exhaustive()
    }
}

impl AsyncControlSession for ControlSession {
    fn handle<'a>(
        &'a mut self,
        request: Vec<u8>,
    ) -> Pin<Box<dyn Future<Output = FaultResult<Vec<u8>>> + Send + 'a>> {
        Box::pin(async move { self.handle_message(&request) })
    }
}

impl ControlSession {
    pub(crate) fn new(host: Arc<HostCore>, authority: Arc<HostAuthority>) -> Self {
        Self {
            host,
            authority,
            active: None,
            last_command_sequence: 0,
            next_status_sequence: 1,
            last_worker_status_sequence: 0,
        }
    }

    fn handle_message(&mut self, request: &[u8]) -> FaultResult<Vec<u8>> {
        let command = wire::WorkerCommand::decode(request).map_err(|error| {
            Fault::invalid_argument("worker control protobuf is invalid").with_source(error)
        })?;
        let wire_status = self.handle_command(command)?;
        let mut encoded = Vec::with_capacity(wire_status.encoded_len());
        wire_status.encode(&mut encoded).map_err(|error| {
            Fault::new(Code::Internal, "worker status encoding failed").with_source(error)
        })?;
        Ok(encoded)
    }

    pub(crate) fn handle_command(
        &mut self,
        command: wire::WorkerCommand,
    ) -> FaultResult<wire::WorkerStatus> {
        self.validate_sequence(command.sequence)?;
        let now = unix_millis()?;
        let status = match command
            .command
            .ok_or_else(|| Fault::invalid_argument("worker control command is missing"))?
        {
            wire::worker_command::Command::Start(start) => self.start(start, now)?,
            wire::worker_command::Command::Cancel(cancel) => self.cancel(cancel, now)?,
            wire::worker_command::Command::Drain(drain) => self.drain(drain, now)?,
            wire::worker_command::Command::Heartbeat(heartbeat) => {
                self.heartbeat(&heartbeat, now)?
            }
        };
        Ok(protocol::worker_status(&status))
    }

    fn start(&mut self, start: wire::StartCommand, now: u64) -> FaultResult<WorkerStatus> {
        if self.active.is_some() {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "worker control connection already owns an active execution",
            ));
        }
        if start.operation.is_empty()
            || start.operation.len() > 256
            || start.operation.trim() != start.operation
        {
            return Err(Fault::invalid_argument("worker operation is invalid"));
        }
        let ticket = protocol::execution_ticket(start.ticket.ok_or_else(|| {
            Fault::invalid_argument("start command is missing execution ticket")
        })?)?;
        let inputs = start
            .inputs
            .into_iter()
            .map(protocol::buffer_descriptor)
            .collect::<FaultResult<Vec<_>>>()?;
        let session = self.authority.begin_invocation(
            &self.host,
            HostInvocation {
                ticket,
                batches: Vec::new(),
                inputs,
            },
            now,
        )?;
        self.active = Some(session);
        self.status(now, "execution admitted")
    }

    fn cancel(&mut self, cancel: wire::CancelCommand, now: u64) -> FaultResult<WorkerStatus> {
        validate_reason_deadline(&cancel.reason, cancel.deadline_unix_millis, now)?;
        self.active_mut()?.cancel(cancel.reason)?;
        self.status(now, "execution cancelled")
    }

    fn drain(&mut self, drain: wire::DrainCommand, now: u64) -> FaultResult<WorkerStatus> {
        validate_reason_deadline(&drain.reason, drain.deadline_unix_millis, now)?;
        self.active_mut()?.drain(drain.reason)?;
        self.status(now, "execution draining")
    }

    fn heartbeat(
        &mut self,
        heartbeat: &wire::HeartbeatCommand,
        now: u64,
    ) -> FaultResult<WorkerStatus> {
        if heartbeat.requested_at_unix_millis == 0 || heartbeat.requested_at_unix_millis > now {
            return Err(Fault::invalid_argument(
                "heartbeat request timestamp is invalid",
            ));
        }
        self.status(now, "heartbeat")
    }

    fn active_mut(&mut self) -> FaultResult<&mut crate::ExecutionSession> {
        self.active.as_mut().ok_or_else(|| {
            Fault::new(
                Code::FailedPrecondition,
                "worker control connection has no active execution",
            )
        })
    }

    fn status(&mut self, now: u64, message: &str) -> FaultResult<WorkerStatus> {
        self.status_with(now, message, Vec::new(), None)
    }

    fn status_with(
        &mut self,
        now: u64,
        message: &str,
        outputs: Vec<mindclade_worker_protocol::BufferDescriptor>,
        diagnostic_artifact: Option<Digest>,
    ) -> FaultResult<WorkerStatus> {
        let active = self.active.as_ref().ok_or_else(|| {
            Fault::new(
                Code::FailedPrecondition,
                "worker control connection has no active execution",
            )
        })?;
        let sequence = self.next_status_sequence;
        self.next_status_sequence = sequence
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "worker status sequence exhausted"))?;
        Ok(WorkerStatus {
            sequence,
            ticket_id: active.ticket_id().to_owned(),
            fencing_token: active.fencing_token(),
            state: active.state(),
            observed_unix_millis: now,
            message: message.to_owned(),
            outputs,
            diagnostic_artifact,
        })
    }

    pub(crate) fn observe_worker_status(
        &mut self,
        message: wire::WorkerStatus,
        maximum_output_bytes: u64,
        now: u64,
    ) -> FaultResult<wire::WorkerStatus> {
        let status = protocol::worker_status_domain(message)?;
        mindclade_worker_protocol::status::validate(
            &status,
            now,
            self.host.config().maximum_input_buffers,
        )?;
        let active = self.active.as_ref().ok_or_else(|| {
            Fault::new(
                Code::FailedPrecondition,
                "worker control connection has no active execution",
            )
        })?;
        if status.sequence <= self.last_worker_status_sequence
            || status.ticket_id != active.ticket_id()
            || status.fencing_token != active.fencing_token()
        {
            return Err(Fault::new(
                Code::Conflict,
                "model-worker status sequence or execution identity is invalid",
            ));
        }
        let output_bytes = status.outputs.iter().try_fold(0_u64, |total, output| {
            total.checked_add(output.range.length()).ok_or_else(|| {
                Fault::new(Code::ResourceExhausted, "model-worker output size overflow")
            })
        })?;
        if output_bytes > maximum_output_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "model-worker outputs exceed the signed execution budget",
            ));
        }
        self.last_worker_status_sequence = status.sequence;
        match status.state {
            WorkerState::Running => {}
            WorkerState::Draining => self.active_mut()?.drain("model worker is draining")?,
            WorkerState::Completed => self.active_mut()?.commit()?,
            WorkerState::Cancelled => self
                .active_mut()?
                .cancel("model worker acknowledged cancellation")?,
            WorkerState::Failed => self.active_mut()?.fail("model worker reported failure")?,
            // A model worker only reaches these phases before it is admitted
            // or while the host itself is driving a transition, so seeing one
            // in a status report means the worker and the host disagree.
            WorkerState::Created
            | WorkerState::Starting
            | WorkerState::Ready
            | WorkerState::Leased
            | WorkerState::Committing
            | WorkerState::Recovering
            | WorkerState::Cancelling => {
                return Err(Fault::new(
                    Code::FailedPrecondition,
                    "model-worker reported an invalid execution state",
                ));
            }
        }
        let message_text = if status.message.is_empty() {
            "model-worker status"
        } else {
            &status.message
        };
        self.status_with(
            now,
            message_text,
            status.outputs,
            status.diagnostic_artifact,
        )
        .map(|status| protocol::worker_status(&status))
    }

    pub(crate) fn fail_execution(
        &mut self,
        reason: &str,
        now: u64,
    ) -> FaultResult<wire::WorkerStatus> {
        self.active_mut()?.fail(reason)?;
        self.status(now, reason)
            .map(|status| protocol::worker_status(&status))
    }

    pub(crate) fn force_cancel(
        &mut self,
        reason: &str,
        now: u64,
    ) -> FaultResult<wire::WorkerStatus> {
        self.active_mut()?.cancel(reason)?;
        self.status(now, reason)
            .map(|status| protocol::worker_status(&status))
    }

    pub(crate) fn validate_cancel_command(
        &mut self,
        command: &wire::WorkerCommand,
        now: u64,
    ) -> FaultResult<()> {
        self.validate_sequence(command.sequence)?;
        match command.command.as_ref() {
            Some(wire::worker_command::Command::Cancel(cancel)) => {
                validate_reason_deadline(&cancel.reason, cancel.deadline_unix_millis, now)
            }
            _ => Err(Fault::invalid_argument(
                "expected a worker cancellation command",
            )),
        }
    }

    pub(crate) fn next_command_sequence(&self) -> FaultResult<u64> {
        self.last_command_sequence
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "worker command sequence exhausted"))
    }

    fn validate_sequence(&mut self, sequence: u64) -> FaultResult<()> {
        if sequence == 0 || sequence <= self.last_command_sequence {
            return Err(Fault::new(
                Code::Conflict,
                "worker command sequence is stale or invalid",
            ));
        }
        self.last_command_sequence = sequence;
        Ok(())
    }
}

impl Drop for ControlSession {
    fn drop(&mut self) {
        if let Some(active) = self.active.as_ref()
            && !matches!(
                active.state(),
                WorkerState::Completed | WorkerState::Cancelled | WorkerState::Failed
            )
        {
            let _ = active.cancel("worker control connection closed");
        }
    }
}

fn validate_reason_deadline(reason: &str, deadline: u64, now: u64) -> FaultResult<()> {
    if reason.is_empty() || reason.len() > 1024 || reason.trim() != reason {
        return Err(Fault::invalid_argument("worker control reason is invalid"));
    }
    if deadline <= now {
        return Err(Fault::new(
            Code::DeadlineExceeded,
            "worker control deadline has expired",
        ));
    }
    Ok(())
}

fn node_resources(snapshot: &Snapshot) -> FaultResult<ResourceVector> {
    // The optional capacities default to zero, which `ResourceVector` reads as
    // "unconstrained". They are declared with defaults rather than read as
    // `Option` so the whole node envelope stays visible in the catalog.
    Ok(ResourceVector::new()
        .set(
            ResourceKind::CpuMillis,
            snapshot.u64_positive("node.cpu.millis")?,
        )
        .set(
            ResourceKind::ResidentMemoryBytes,
            snapshot.u64_positive("node.memory.bytes")?,
        )
        .set(
            ResourceKind::PinnedMemoryBytes,
            snapshot.u64("node.pinned.memory.bytes")?,
        )
        .set(
            ResourceKind::SharedMemoryBytes,
            snapshot.u64("node.shared.memory.bytes")?,
        )
        .set(
            ResourceKind::LocalDiskBytes,
            snapshot.u64("node.disk.bytes")?,
        )
        .set(
            ResourceKind::OpenFileDescriptors,
            snapshot.u64_positive("node.open.fds")?,
        )
        .set(
            ResourceKind::ObjectStoreRequests,
            snapshot.u64("node.object.requests")?,
        )
        .set(
            ResourceKind::QueuedRequests,
            snapshot.u64("node.queued.requests")?,
        )
        .set(
            ResourceKind::Processes,
            snapshot.u64_positive("node.processes")?,
        )
        .set(
            ResourceKind::CpuThreads,
            snapshot.u64_positive("node.cpu.threads")?,
        )
        .set(
            ResourceKind::GpuMemoryEstimateBytes,
            snapshot.u64_positive("node.gpu.memory.bytes")?,
        )
        .set(
            ResourceKind::CheckpointStagingBytes,
            snapshot.u64("node.checkpoint.staging.bytes")?,
        )
        .set(
            ResourceKind::TelemetrySpoolBytes,
            snapshot.u64("node.telemetry.spool.bytes")?,
        ))
}

/// The preloaded-model group is present as a unit or absent as a unit.
///
/// `Snapshot::is_set` distinguishes an operator-supplied value from the declared
/// default, including an operator-supplied *empty* value, which is what the
/// replaced `env::var(..).ok()` reads did. A partially configured group is
/// rejected rather than half-applied: a host that silently skips preloading is a
/// capacity incident nobody gets paged for.
fn model_spec(lookup: &EnvSource, snapshot: &Snapshot) -> FaultResult<Option<ModelSpec>> {
    const GROUP: [&str; 4] = [
        "model.bundle.digest",
        "model.worker.executable",
        "model.worker.socket",
        "model.worker.config",
    ];
    let mut configured = 0_usize;
    for key in GROUP {
        if snapshot.is_set(key)? {
            configured += 1;
        }
    }
    if configured == 0 {
        return Ok(None);
    }
    if configured != GROUP.len() {
        return Err(Fault::invalid_argument(
            "preloaded model digest, worker executable, worker socket, and worker config must be configured together",
        ));
    }
    let model = config::model_catalog()?.load(&[&config::bind_model(lookup.clone())])?;
    build_model_spec(
        snapshot.raw("model.bundle.digest")?,
        snapshot.string("model.worker.executable")?,
        snapshot.string("model.worker.socket")?,
        snapshot.string("model.worker.config")?,
        snapshot.string("host.socket")?,
        snapshot.string("host.grpc.socket")?,
        model.u64_positive("model.gpu.memory.bytes")?,
        model.u64("model.pinned.memory.bytes")?,
    )
    .map(Some)
}

#[allow(clippy::too_many_arguments)]
fn build_model_spec(
    digest: &str,
    executable: String,
    control_socket: String,
    config_path: String,
    host_socket: String,
    host_grpc_socket: String,
    minimum_gpu_memory_bytes: u64,
    pinned_memory_bytes: u64,
) -> FaultResult<ModelSpec> {
    let model_digest = Digest::from_str(digest).map_err(|error| {
        Fault::invalid_argument("preloaded model digest is invalid").with_source(error)
    })?;
    let control_socket = PathBuf::from(control_socket);
    let host_socket = PathBuf::from(host_socket);
    let host_grpc_socket = PathBuf::from(host_grpc_socket);
    if !control_socket.is_absolute()
        || control_socket.as_os_str().as_encoded_bytes().len() > MAX_SOCKET_PATH_BYTES
    {
        return Err(Fault::invalid_argument(
            "model-worker socket path must be bounded and absolute",
        ));
    }
    if control_socket == host_socket || control_socket == host_grpc_socket {
        return Err(Fault::invalid_argument(
            "model-worker socket must differ from runtime-host sockets",
        ));
    }
    let config_path = PathBuf::from(config_path);
    if !config_path.is_absolute() {
        return Err(Fault::invalid_argument(
            "model-worker config path must be absolute",
        ));
    }
    let mut environment = BTreeMap::new();
    environment.insert(
        "MINDCLADE_MODEL_WORKER_CONFIG".to_owned(),
        config_path.to_string_lossy().into_owned(),
    );
    environment.insert(
        "MINDCLADE_MODEL_WORKER_SOCKET".to_owned(),
        control_socket.to_string_lossy().into_owned(),
    );
    let spec = ModelSpec {
        model_digest,
        minimum_gpu_memory_bytes,
        pinned_memory_bytes,
        control_socket,
        process: ProcessSpec {
            name: "model-worker".to_owned(),
            executable,
            arguments: Vec::new(),
            environment,
        },
    };
    spec.validate()?;
    Ok(spec)
}

fn read_message<M: Message + Default>(path: &PathBuf) -> FaultResult<M> {
    let file = std::fs::File::open(path).map_err(|error| {
        Fault::new(Code::NotFound, "runtime policy file is unavailable").with_source(error)
    })?;
    let metadata = file.metadata().map_err(|error| {
        Fault::new(Code::Unavailable, "runtime policy file inspection failed").with_source(error)
    })?;
    if !metadata.is_file() || metadata.len() == 0 || metadata.len() > MAX_POLICY_FILE_BYTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "runtime policy file size is invalid",
        ));
    }
    let capacity = usize::try_from(metadata.len()).map_err(|_| {
        Fault::new(
            Code::ResourceExhausted,
            "runtime policy file exceeds platform limits",
        )
    })?;
    let mut bytes = Vec::with_capacity(capacity);
    file.take(MAX_POLICY_FILE_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|error| {
            Fault::new(Code::Unavailable, "runtime policy file read failed").with_source(error)
        })?;
    if bytes.is_empty()
        || u64::try_from(bytes.len()).map_or(true, |length| length > MAX_POLICY_FILE_BYTES)
    {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "runtime policy file changed beyond its size bound while reading",
        ));
    }
    M::decode(bytes.as_slice()).map_err(|error| {
        Fault::invalid_argument("runtime policy protobuf is invalid").with_source(error)
    })
}

fn decode_32_byte_hex(value: &str) -> FaultResult<[u8; 32]> {
    if value.len() != 64 || !value.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        return Err(Fault::invalid_argument(
            "runtime Ed25519 public key must be 64 hexadecimal characters",
        ));
    }
    let mut output = [0_u8; 32];
    for (index, slot) in output.iter_mut().enumerate() {
        let offset = index * 2;
        *slot = u8::from_str_radix(&value[offset..offset + 2], 16).map_err(|error| {
            Fault::invalid_argument("runtime Ed25519 public key is invalid").with_source(error)
        })?;
    }
    Ok(output)
}

fn unix_millis() -> FaultResult<u64> {
    let elapsed = std::time::SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| {
            Fault::new(
                Code::FailedPrecondition,
                "runtime-host clock is before Unix epoch",
            )
        })?;
    u64::try_from(elapsed.as_millis()).map_err(|_| {
        Fault::new(
            Code::OutOfRange,
            "runtime-host clock exceeds u64 milliseconds",
        )
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_exact_ed25519_public_key() {
        let key = decode_32_byte_hex(&"ab".repeat(32)).expect("valid key");
        assert_eq!(key, [0xab; 32]);
        assert!(decode_32_byte_hex("ab").is_err());
    }

    #[test]
    fn model_spec_forwards_only_the_bounded_worker_contract() {
        let spec = build_model_spec(
            &format!("sha256:{}", "ab".repeat(32)),
            "/opt/mindclade/model-worker".to_owned(),
            "/run/mindclade/model-worker.sock".to_owned(),
            "/etc/mindclade/model-worker.json".to_owned(),
            "/run/mindclade/runtime-host.sock".to_owned(),
            "/run/mindclade/runtime-host-grpc.sock".to_owned(),
            80 * 1024 * 1024 * 1024,
            1024,
        )
        .expect("valid model spec");
        assert_eq!(
            spec.process.environment,
            BTreeMap::from([
                (
                    "MINDCLADE_MODEL_WORKER_CONFIG".to_owned(),
                    "/etc/mindclade/model-worker.json".to_owned(),
                ),
                (
                    "MINDCLADE_MODEL_WORKER_SOCKET".to_owned(),
                    "/run/mindclade/model-worker.sock".to_owned(),
                ),
            ])
        );
        assert!(spec.process.arguments.is_empty());
    }

    #[test]
    fn model_spec_rejects_relative_config_and_socket_collisions() {
        let digest = format!("sha256:{}", "ab".repeat(32));
        assert!(
            build_model_spec(
                &digest,
                "/opt/mindclade/model-worker".to_owned(),
                "/run/mindclade/model-worker.sock".to_owned(),
                "relative.json".to_owned(),
                "/run/mindclade/runtime-host.sock".to_owned(),
                "/run/mindclade/runtime-host-grpc.sock".to_owned(),
                1,
                0,
            )
            .is_err()
        );
        assert!(
            build_model_spec(
                &digest,
                "/opt/mindclade/model-worker".to_owned(),
                "/run/mindclade/runtime-host.sock".to_owned(),
                "/etc/mindclade/model-worker.json".to_owned(),
                "/run/mindclade/runtime-host.sock".to_owned(),
                "/run/mindclade/runtime-host-grpc.sock".to_owned(),
                1,
                0,
            )
            .is_err()
        );
    }
}
