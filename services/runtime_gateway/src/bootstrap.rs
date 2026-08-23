// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Fail-closed runtime-gateway process bootstrap.
//!
//! The binary accepts only immutable signed policy artifacts and bounded local
//! configuration. Global policy remains owned by the Go control plane.

use crate::grpc;
use crate::{
    GatewayAuthority, GatewayComponent, GatewayConfig, GatewayCore, GatewayHealth,
    network::{self, GatewayNetworkState},
    protocol,
};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_protocols::runtime::v1 as wire;
use mindclade_servicekit::{Service, signals};
use mindclade_worker_protocol::Ed25519VerificationKey;
use prost::Message;
use std::io::Read;
use std::{env, fs, net::SocketAddr, path::PathBuf, sync::Arc, time::UNIX_EPOCH};
use tokio::sync::watch;

const MAX_POLICY_FILE_BYTES: u64 = 16 * 1024 * 1024;

#[derive(Clone, Debug)]
pub struct BootstrapConfig {
    pub listen_address: SocketAddr,
    pub grpc_listen_address: SocketAddr,
    pub key_id: String,
    pub public_key: [u8; 32],
    pub key_not_before_unix_millis: u64,
    pub key_not_after_unix_millis: u64,
    pub route_snapshot_path: PathBuf,
    pub revocation_snapshot_path: PathBuf,
    pub gateway: GatewayConfig,
}

impl BootstrapConfig {
    pub fn from_env() -> FaultResult<Self> {
        let listen_address = required("MINDCLADE_RUNTIME_GATEWAY_ADDR")?
            .parse::<SocketAddr>()
            .map_err(|error| {
                Fault::invalid_argument("runtime gateway address is invalid").with_source(error)
            })?;
        let grpc_listen_address = required("MINDCLADE_RUNTIME_GATEWAY_GRPC_ADDR")?
            .parse::<SocketAddr>()
            .map_err(|error| {
                Fault::invalid_argument("runtime gateway gRPC address is invalid")
                    .with_source(error)
            })?;
        if grpc_listen_address == listen_address {
            return Err(Fault::invalid_argument(
                "runtime gateway HTTP and gRPC addresses must differ",
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
        let route_snapshot_path = absolute_path("MINDCLADE_RUNTIME_ROUTE_SNAPSHOT_FILE")?;
        let revocation_snapshot_path = absolute_path("MINDCLADE_RUNTIME_REVOCATION_SNAPSHOT_FILE")?;
        let pod_memory_limit_bytes = parse_u64("MINDCLADE_POD_MEMORY_LIMIT_BYTES")?;
        let mut gateway = GatewayConfig::default().with_memory_limit(pod_memory_limit_bytes)?;
        if let Some(value) = optional_u64("MINDCLADE_RUNTIME_GATEWAY_REQUEST_BYTES")? {
            gateway.request_buffer_budget_bytes = value;
        }
        if let Some(value) = optional_u64("MINDCLADE_RUNTIME_GATEWAY_RESPONSE_BYTES")? {
            gateway.response_buffer_budget_bytes = value;
        }
        gateway.execution_enabled =
            optional_bool("MINDCLADE_RUNTIME_EXECUTION_ENABLED")?.unwrap_or(false);
        gateway.validate()?;
        Ok(Self {
            listen_address,
            grpc_listen_address,
            key_id,
            public_key,
            key_not_before_unix_millis,
            key_not_after_unix_millis,
            route_snapshot_path,
            revocation_snapshot_path,
            gateway,
        })
    }
}

pub async fn run(config: BootstrapConfig) -> FaultResult<()> {
    let now = unix_millis()?;
    let revocations = protocol::revocation_snapshot(read_message::<wire::RevocationSnapshot>(
        &config.revocation_snapshot_path,
    )?)?;
    let route = protocol::route_snapshot(read_message::<wire::RouteSnapshot>(
        &config.route_snapshot_path,
    )?)?;
    let authority = GatewayAuthority::from_ed25519_keys(
        [Ed25519VerificationKey {
            key_id: config.key_id,
            public_key: config.public_key,
            not_before_unix_millis: config.key_not_before_unix_millis,
            not_after_unix_millis: config.key_not_after_unix_millis,
            disabled: false,
        }],
        revocations,
        now,
    )?;
    authority.install_route(route, now)?;

    let health = Arc::new(GatewayHealth::new());
    let listen_address = config.listen_address;
    let grpc_listen_address = config.grpc_listen_address;
    let request_buffer_budget_bytes = config.gateway.request_buffer_budget_bytes;
    let response_buffer_budget_bytes = config.gateway.response_buffer_budget_bytes;
    let execution_enabled = config.gateway.execution_enabled;
    let core = Arc::new(GatewayCore::new(
        config.gateway,
        authority.policy(),
        health.clone(),
    )?);

    // servicekit owns start order, drain, and stop. Readiness is asserted by
    // the component's start hook rather than inline here, so the process cannot
    // advertise itself as accepting before the lifecycle says it started.
    let mut service = Service::new();
    service.register(Box::new(GatewayComponent::new(
        core.clone(),
        health.clone(),
    )))?;
    service.start()?;

    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let signal = tokio::spawn(drain_on_termination(
        core.clone(),
        shutdown_tx,
        signals::termination_requested(),
    ));
    let state = GatewayNetworkState::new(
        core.clone(),
        request_buffer_budget_bytes,
        response_buffer_budget_bytes,
        execution_enabled,
    )?;
    let network_state = state.clone();
    let network_shutdown = shutdown_rx.clone();
    let result = tokio::try_join!(
        async move {
            network::serve(listen_address, network_state, network_shutdown)
                .await
                .map_err(|error| {
                    Fault::new(Code::Unavailable, "runtime gateway network server failed")
                        .with_source(error)
                })
        },
        grpc::serve(grpc_listen_address, state, shutdown_rx),
    )
    .map(|_| ());
    signal.abort();

    // Drain and stop run in reverse registration order here. The serve error, if
    // any, outranks a shutdown fault: it is the reason the process is ending.
    let shutdown = service.stop();
    result.and(shutdown)
}

/// Report unready and close admission the moment termination is requested,
/// BEFORE the serve loops are told to unwind.
///
/// The prior defect was ordering, not absence. The only drain was the reverse
/// pass inside `service.stop()`, which runs *after* `try_join!` returns -- that
/// is, after both serve loops have finished their graceful close, up to
/// `network::GATEWAY_DRAIN_TIMEOUT` (30s) later. For that entire window
/// `/readyz` still answered 200, because it renders
/// `GatewayHealthSnapshot::ready()` and `accepting` was still true. A load
/// balancer or Kubernetes readiness probe therefore kept this pod in rotation
/// and kept aiming new requests at a gateway that was already tearing down.
/// `GatewayCore::admit_request` also still admitted, for the same reason.
///
/// `runtime_host` already drains at signal time; this is that same ordering.
/// `Component::drain` and `GatewayCore::begin_drain` are both idempotent, so
/// the reverse-order pass in `service.stop()` repeating this is harmless -- and
/// keeping it there is what still covers a serve loop that fails on its own
/// rather than on a signal.
pub async fn drain_on_termination(
    core: Arc<GatewayCore>,
    shutdown: watch::Sender<bool>,
    termination: impl Future<Output = ()> + Send,
) {
    termination.await;
    core.begin_drain();
    let _ = shutdown.send(true);
}

fn read_message<M: Message + Default>(path: &PathBuf) -> FaultResult<M> {
    let file = fs::File::open(path).map_err(|error| {
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

fn required(name: &'static str) -> FaultResult<String> {
    let value = env::var(name).map_err(|_| {
        Fault::new(
            Code::FailedPrecondition,
            "required runtime environment variable is missing",
        )
        .with_context("variable", name)
    })?;
    if value.is_empty() || value != value.trim() || value.len() > 4096 {
        return Err(
            Fault::invalid_argument("runtime environment value is invalid")
                .with_context("variable", name),
        );
    }
    Ok(value)
}

fn parse_u64(name: &'static str) -> FaultResult<u64> {
    required(name)?.parse::<u64>().map_err(|error| {
        Fault::invalid_argument("runtime integer environment value is invalid")
            .with_context("variable", name)
            .with_source(error)
    })
}

fn optional_u64(name: &'static str) -> FaultResult<Option<u64>> {
    match env::var(name) {
        Ok(value) => value.parse::<u64>().map(Some).map_err(|error| {
            Fault::invalid_argument("runtime integer environment value is invalid")
                .with_context("variable", name)
                .with_source(error)
        }),
        Err(env::VarError::NotPresent) => Ok(None),
        Err(error) => Err(Fault::invalid_argument(
            "runtime environment value is not valid Unicode",
        )
        .with_context("variable", name)
        .with_source(error)),
    }
}

fn optional_bool(name: &'static str) -> FaultResult<Option<bool>> {
    match env::var(name) {
        Ok(value) => match value.as_str() {
            "true" | "1" => Ok(Some(true)),
            "false" | "0" => Ok(Some(false)),
            _ => Err(
                Fault::invalid_argument("runtime boolean environment value is invalid")
                    .with_context("variable", name),
            ),
        },
        Err(env::VarError::NotPresent) => Ok(None),
        Err(error) => Err(Fault::invalid_argument(
            "runtime environment value is not valid Unicode",
        )
        .with_context("variable", name)
        .with_source(error)),
    }
}

fn absolute_path(name: &'static str) -> FaultResult<PathBuf> {
    let path = PathBuf::from(required(name)?);
    if !path.is_absolute() {
        return Err(
            Fault::invalid_argument("runtime policy path must be absolute")
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
                "runtime gateway clock is before Unix epoch",
            )
        })?;
    u64::try_from(elapsed.as_millis()).map_err(|_| {
        Fault::new(
            Code::OutOfRange,
            "runtime gateway clock exceeds u64 milliseconds",
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
