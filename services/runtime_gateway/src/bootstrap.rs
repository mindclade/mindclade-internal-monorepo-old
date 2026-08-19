// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Fail-closed runtime-gateway process bootstrap.
//!
//! The binary accepts only immutable signed policy artifacts and bounded local
//! configuration. Global policy remains owned by the Go control plane.

use crate::{
    GatewayAuthority, GatewayConfig, GatewayCore, GatewayHealth,
    network::{self, GatewayNetworkState},
    protocol,
};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_protocols::runtime::v1 as wire;
use mindclade_worker_protocol::Ed25519VerificationKey;
use prost::Message;
use std::{env, fs, net::SocketAddr, path::PathBuf, sync::Arc, time::UNIX_EPOCH};
use tokio::sync::watch;

const MAX_POLICY_FILE_BYTES: u64 = 16 * 1024 * 1024;

#[derive(Clone, Debug)]
pub struct BootstrapConfig {
    pub listen_address: SocketAddr,
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
        let gateway = GatewayConfig::default();
        gateway.validate()?;
        Ok(Self {
            listen_address,
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
    let core = Arc::new(GatewayCore::new(
        config.gateway,
        authority.policy(),
        health.clone(),
    )?);
    health.set_policy_fresh(true);
    health.set_accepting(true);

    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let signal = tokio::spawn(async move {
        termination_requested().await;
        let _ = shutdown_tx.send(true);
    });
    let result = network::serve(
        config.listen_address,
        GatewayNetworkState::new(core.clone()),
        shutdown_rx,
    )
    .await
    .map_err(|error| {
        Fault::new(Code::Unavailable, "runtime gateway network server failed").with_source(error)
    });
    core.begin_drain();
    signal.abort();
    result
}

/// Resolves when the operating system asks this process to terminate.
///
/// SIGTERM is the signal that matters. Kubernetes, Cloud Run, and `docker stop`
/// all send it and none of them send SIGINT, so waiting on `ctrl_c` alone means
/// the drain below never runs: the process sits idle through the whole
/// termination grace period and is then killed with SIGKILL, requests still in
/// flight. SIGINT stays handled for interactive runs.
#[cfg(unix)]
async fn termination_requested() {
    use tokio::signal::unix::{SignalKind, signal};

    let Ok(mut terminate) = signal(SignalKind::terminate()) else {
        return interrupt_requested().await;
    };
    tokio::select! {
        _ = terminate.recv() => {}
        () = interrupt_requested() => {}
    }
}

#[cfg(not(unix))]
async fn termination_requested() {
    interrupt_requested().await;
}

/// SIGINT, and never resolving if the handler cannot be registered.
///
/// A failed registration must not resolve: the caller treats a resolve as
/// "shutdown requested", so reporting one here would stop the process during
/// startup rather than report the signal it could not hear.
async fn interrupt_requested() {
    if tokio::signal::ctrl_c().await.is_err() {
        std::future::pending::<()>().await;
    }
}

fn read_message<M: Message + Default>(path: &PathBuf) -> FaultResult<M> {
    let metadata = fs::metadata(path).map_err(|error| {
        Fault::new(Code::NotFound, "runtime policy file is unavailable").with_source(error)
    })?;
    if !metadata.is_file() || metadata.len() == 0 || metadata.len() > MAX_POLICY_FILE_BYTES {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "runtime policy file size is invalid",
        ));
    }
    let bytes = fs::read(path).map_err(|error| {
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

    /// Guards the regression this fix addresses: `ctrl_c` alone listens for
    /// SIGINT, and every orchestrator these binaries run under sends SIGTERM.
    #[cfg(unix)]
    #[tokio::test]
    async fn sigterm_requests_termination() {
        use std::{process::Command, time::Duration};
        use tokio::signal::unix::{SignalKind, signal};

        // Install the handler BEFORE raising. Until some stream exists the
        // default disposition is still in place, and the raise below would
        // terminate the test binary instead of being delivered to Tokio.
        let _installed = signal(SignalKind::terminate()).expect("SIGTERM handler installs");

        let waiter = tokio::spawn(termination_requested());
        // Let the spawned task reach its own registration before signalling.
        tokio::task::yield_now().await;

        let status = Command::new("kill")
            .args(["-TERM", &std::process::id().to_string()])
            .status()
            .expect("kill runs");
        assert!(status.success(), "could not signal the test process");

        tokio::time::timeout(Duration::from_secs(5), waiter)
            .await
            .expect("termination_requested must resolve on SIGTERM")
            .expect("waiter task joins");
    }
}
