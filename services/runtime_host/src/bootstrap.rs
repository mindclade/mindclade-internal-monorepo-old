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
use crate::protocol;
use crate::{
    HostAuthority, HostConfig, HostCore, HostHealth, ModelSpec, ProcessSpec, StdProcessLauncher,
};
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_gpu_host::DeviceCapability;
use mindclade_protocols::runtime::v1 as wire;
use mindclade_runtime_core::{ResourceKind, ResourceVector};
use mindclade_serving_runtime::host::HostInvocation;
use mindclade_worker_protocol::{Ed25519VerificationKey, WorkerState, WorkerStatus};
use prost::Message;
use std::collections::BTreeMap;
use std::env;
use std::future::Future;
use std::path::PathBuf;
use std::pin::Pin;
use std::str::FromStr;
use std::sync::Arc;
use std::time::UNIX_EPOCH;
use tokio::sync::watch;

const MAX_POLICY_FILE_BYTES: u64 = 16 * 1024 * 1024;
const MAX_SOCKET_PATH_BYTES: usize = 100;

#[derive(Clone, Debug)]
pub struct BootstrapConfig {
    pub socket_path: PathBuf,
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
    pub fn from_env() -> FaultResult<Self> {
        let socket_path = absolute_path("MINDCLADE_RUNTIME_HOST_SOCKET")?;
        if socket_path.as_os_str().as_encoded_bytes().len() > MAX_SOCKET_PATH_BYTES {
            return Err(Fault::invalid_argument(
                "runtime-host socket path exceeds platform bound",
            ));
        }
        let key_id = required("MINDCLADE_RUNTIME_KEY_ID")?;
        if key_id.len() > 256 {
            return Err(Fault::invalid_argument("runtime key id exceeds bound"));
        }
        let public_key = decode_32_byte_hex(&required("MINDCLADE_RUNTIME_PUBLIC_KEY_HEX")?)?;
        let key_not_before_unix_millis = parse_u64("MINDCLADE_RUNTIME_KEY_NOT_BEFORE_MS")?;
        let key_not_after_unix_millis = parse_u64("MINDCLADE_RUNTIME_KEY_NOT_AFTER_MS")?;
        if key_not_before_unix_millis >= key_not_after_unix_millis {
            return Err(Fault::invalid_argument(
                "runtime verification-key validity window is invalid",
            ));
        }
        let revocation_snapshot_path = absolute_path("MINDCLADE_RUNTIME_REVOCATION_SNAPSHOT_FILE")?;
        let minimum_policy_epoch = parse_positive_u64("MINDCLADE_RUNTIME_MIN_POLICY_EPOCH")?;
        let minimum_route_version = parse_positive_u64("MINDCLADE_RUNTIME_MIN_ROUTE_VERSION")?;
        let minimum_revocation_epoch =
            parse_positive_u64("MINDCLADE_RUNTIME_MIN_REVOCATION_EPOCH")?;

        let host = HostConfig {
            maximum_processes: parse_positive_u32("MINDCLADE_RUNTIME_MAX_PROCESSES")?,
            maximum_model_slots: parse_positive_u32("MINDCLADE_RUNTIME_MAX_MODEL_SLOTS")?,
            maximum_input_buffers: parse_positive_usize("MINDCLADE_RUNTIME_MAX_INPUT_BUFFERS")?,
            maximum_control_payload_bytes: parse_positive_u64(
                "MINDCLADE_RUNTIME_MAX_CONTROL_BYTES",
            )?,
            node_resources: node_resources_from_env()?,
        };
        host.validate()?;

        let gpu = DeviceCapability {
            vendor: required("MINDCLADE_RUNTIME_GPU_VENDOR")?,
            architecture: required("MINDCLADE_RUNTIME_GPU_ARCH")?,
            total_memory_bytes: parse_positive_u64("MINDCLADE_RUNTIME_GPU_MEMORY_BYTES")?,
        };
        gpu.validate()?;

        let preloaded_model = model_spec_from_env()?;
        Ok(Self {
            socket_path,
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
    health.set_accepting(true);

    let factory: Arc<dyn AsyncControlSessionFactory> = Arc::new(ControlFactory {
        host: core.clone(),
        authority,
    });
    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let drain_core = core.clone();
    let signal = tokio::spawn(async move {
        if tokio::signal::ctrl_c().await.is_ok() {
            drain_core.begin_drain();
            let _ = shutdown_tx.send(true);
        }
    });

    let serve_result =
        async_ipc::serve_unix_sessions(config.socket_path, factory, shutdown_rx).await;
    core.begin_drain();
    signal.abort();
    let shutdown_result = core.shutdown();
    serve_result.and(shutdown_result)
}

struct ControlFactory {
    host: Arc<HostCore>,
    authority: Arc<HostAuthority>,
}

impl AsyncControlSessionFactory for ControlFactory {
    fn open(&self) -> FaultResult<Box<dyn AsyncControlSession>> {
        Ok(Box::new(ControlSession {
            host: self.host.clone(),
            authority: self.authority.clone(),
            active: None,
            last_command_sequence: 0,
            next_status_sequence: 1,
        }))
    }
}

struct ControlSession {
    host: Arc<HostCore>,
    authority: Arc<HostAuthority>,
    active: Option<crate::ExecutionSession>,
    last_command_sequence: u64,
    next_status_sequence: u64,
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
        Box::pin(async move { self.handle_message(request) })
    }
}

impl ControlSession {
    fn handle_message(&mut self, request: Vec<u8>) -> FaultResult<Vec<u8>> {
        let command = wire::WorkerCommand::decode(request.as_slice()).map_err(|error| {
            Fault::invalid_argument("worker control protobuf is invalid").with_source(error)
        })?;
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
                self.heartbeat(heartbeat, now)?
            }
        };
        let wire_status = protocol::worker_status(&status);
        let mut encoded = Vec::with_capacity(wire_status.encoded_len());
        wire_status.encode(&mut encoded).map_err(|error| {
            Fault::new(Code::Internal, "worker status encoding failed").with_source(error)
        })?;
        Ok(encoded)
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
        heartbeat: wire::HeartbeatCommand,
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
            outputs: Vec::new(),
            diagnostic_artifact: None,
        })
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
        if let Some(active) = self.active.as_ref() {
            if !matches!(
                active.state(),
                WorkerState::Completed | WorkerState::Cancelled | WorkerState::Failed
            ) {
                let _ = active.cancel("worker control connection closed");
            }
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

fn node_resources_from_env() -> FaultResult<ResourceVector> {
    Ok(ResourceVector::new()
        .set(
            ResourceKind::CpuMillis,
            parse_positive_u64("MINDCLADE_RUNTIME_NODE_CPU_MILLIS")?,
        )
        .set(
            ResourceKind::ResidentMemoryBytes,
            parse_positive_u64("MINDCLADE_RUNTIME_NODE_MEMORY_BYTES")?,
        )
        .set(
            ResourceKind::PinnedMemoryBytes,
            parse_optional_u64("MINDCLADE_RUNTIME_NODE_PINNED_MEMORY_BYTES")?,
        )
        .set(
            ResourceKind::SharedMemoryBytes,
            parse_optional_u64("MINDCLADE_RUNTIME_NODE_SHARED_MEMORY_BYTES")?,
        )
        .set(
            ResourceKind::LocalDiskBytes,
            parse_optional_u64("MINDCLADE_RUNTIME_NODE_DISK_BYTES")?,
        )
        .set(
            ResourceKind::OpenFileDescriptors,
            parse_positive_u64("MINDCLADE_RUNTIME_NODE_OPEN_FDS")?,
        )
        .set(
            ResourceKind::ObjectStoreRequests,
            parse_optional_u64("MINDCLADE_RUNTIME_NODE_OBJECT_REQUESTS")?,
        )
        .set(
            ResourceKind::QueuedRequests,
            parse_optional_u64("MINDCLADE_RUNTIME_NODE_QUEUED_REQUESTS")?,
        )
        .set(
            ResourceKind::Processes,
            parse_positive_u64("MINDCLADE_RUNTIME_NODE_PROCESSES")?,
        )
        .set(
            ResourceKind::CpuThreads,
            parse_positive_u64("MINDCLADE_RUNTIME_NODE_CPU_THREADS")?,
        )
        .set(
            ResourceKind::GpuMemoryEstimateBytes,
            parse_positive_u64("MINDCLADE_RUNTIME_NODE_GPU_MEMORY_BYTES")?,
        )
        .set(
            ResourceKind::CheckpointStagingBytes,
            parse_optional_u64("MINDCLADE_RUNTIME_NODE_CHECKPOINT_STAGING_BYTES")?,
        )
        .set(
            ResourceKind::TelemetrySpoolBytes,
            parse_optional_u64("MINDCLADE_RUNTIME_NODE_TELEMETRY_SPOOL_BYTES")?,
        ))
}

fn model_spec_from_env() -> FaultResult<Option<ModelSpec>> {
    let digest = env::var("MINDCLADE_RUNTIME_MODEL_BUNDLE_DIGEST").ok();
    let executable = env::var("MINDCLADE_RUNTIME_MODEL_WORKER_EXECUTABLE").ok();
    match (digest, executable) {
        (None, None) => Ok(None),
        (Some(digest), Some(executable)) => {
            let model_digest = Digest::from_str(&digest).map_err(|error| {
                Fault::invalid_argument("preloaded model digest is invalid").with_source(error)
            })?;
            let spec = ModelSpec {
                model_digest,
                minimum_gpu_memory_bytes: parse_positive_u64(
                    "MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES",
                )?,
                pinned_memory_bytes: parse_optional_u64(
                    "MINDCLADE_RUNTIME_MODEL_PINNED_MEMORY_BYTES",
                )?,
                process: ProcessSpec {
                    name: "model-worker".to_owned(),
                    executable,
                    arguments: Vec::new(),
                    environment: BTreeMap::new(),
                },
            };
            spec.validate()?;
            Ok(Some(spec))
        }
        _ => Err(Fault::invalid_argument(
            "preloaded model digest and worker executable must be configured together",
        )),
    }
}

fn read_message<M: Message + Default>(path: &PathBuf) -> FaultResult<M> {
    let metadata = std::fs::metadata(path).map_err(|error| {
        Fault::new(Code::NotFound, "runtime policy file is unavailable").with_source(error)
    })?;
    if !metadata.is_file() || metadata.len() == 0 || metadata.len() > MAX_POLICY_FILE_BYTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "runtime policy file size is invalid",
        ));
    }
    let bytes = std::fs::read(path).map_err(|error| {
        Fault::new(Code::Unavailable, "runtime policy file read failed").with_source(error)
    })?;
    M::decode(bytes.as_slice()).map_err(|error| {
        Fault::invalid_argument("runtime policy protobuf is invalid").with_source(error)
    })
}

fn required(name: &'static str) -> FaultResult<String> {
    let value = env::var(name).map_err(|_| {
        Fault::new(
            Code::FailedPrecondition,
            "required runtime-host environment variable is missing",
        )
        .with_context("variable", name)
    })?;
    if value.is_empty() || value != value.trim() || value.len() > 4096 {
        return Err(
            Fault::invalid_argument("runtime-host environment value is invalid")
                .with_context("variable", name),
        );
    }
    Ok(value)
}

fn parse_u64(name: &'static str) -> FaultResult<u64> {
    required(name)?.parse::<u64>().map_err(|error| {
        Fault::invalid_argument("runtime-host integer environment value is invalid")
            .with_context("variable", name)
            .with_source(error)
    })
}

fn parse_positive_u64(name: &'static str) -> FaultResult<u64> {
    let value = parse_u64(name)?;
    if value == 0 {
        return Err(
            Fault::invalid_argument("runtime-host integer must be positive")
                .with_context("variable", name),
        );
    }
    Ok(value)
}

fn parse_optional_u64(name: &'static str) -> FaultResult<u64> {
    match env::var(name) {
        Ok(value) if !value.is_empty() => value.parse::<u64>().map_err(|error| {
            Fault::invalid_argument("runtime-host optional integer environment value is invalid")
                .with_context("variable", name)
                .with_source(error)
        }),
        Ok(_) | Err(env::VarError::NotPresent) => Ok(0),
        Err(error) => Err(
            Fault::invalid_argument("runtime-host environment value is not Unicode")
                .with_context("variable", name)
                .with_source(error),
        ),
    }
}

fn parse_positive_u32(name: &'static str) -> FaultResult<u32> {
    let value = parse_positive_u64(name)?;
    u32::try_from(value).map_err(|_| {
        Fault::new(Code::OutOfRange, "runtime-host integer exceeds u32")
            .with_context("variable", name)
    })
}

fn parse_positive_usize(name: &'static str) -> FaultResult<usize> {
    let value = parse_positive_u64(name)?;
    usize::try_from(value).map_err(|_| {
        Fault::new(Code::OutOfRange, "runtime-host integer exceeds usize")
            .with_context("variable", name)
    })
}

fn absolute_path(name: &'static str) -> FaultResult<PathBuf> {
    let path = PathBuf::from(required(name)?);
    if !path.is_absolute() {
        return Err(
            Fault::invalid_argument("runtime-host path must be absolute")
                .with_context("variable", name),
        );
    }
    Ok(path)
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
}
