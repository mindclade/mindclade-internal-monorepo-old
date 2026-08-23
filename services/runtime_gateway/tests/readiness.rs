// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! End-to-end readiness transitions driven by real runtime-host reachability.
//!
//! The regression these pin: `GatewayHealthSnapshot::ready()` requires
//! `runtime_host_ready`, and nothing in the production bootstrap ever set it.
//! `/readyz` therefore answered 503 for the entire life of the pod, so every
//! readiness-gated rollout and load-balancer membership decision for this
//! service was permanently blocked. See `docs/runbooks/runtime-gateway-degraded.md`.
//!
//! These tests deliberately go through the real HTTP route rather than reading
//! the flag: the assertion is about what a Kubernetes readiness probe observes
//! on the wire.

use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::FaultResult;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_gateway::network::{self, GatewayNetworkState};
use mindclade_runtime_gateway::{
    GatewayComponent, GatewayConfig, GatewayCore, GatewayHealth, PolicyCache,
};
use mindclade_servicekit::Service;
use mindclade_worker_protocol::{
    DeploymentRoute, DetachedSignature, RevocationSnapshot, RevocationSnapshotClaims, RouteSnapshot,
    RouteSnapshotClaims, SignatureVerifier,
};
use std::collections::BTreeSet;
use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpStream, UnixListener};
use tokio::sync::watch;
use tokio::time::timeout;

/// Bounds every network wait in this file. A probe that hangs is a failure, not
/// a reason for the suite to stall until the harness kills it.
const PROBE_TIMEOUT: Duration = Duration::from_secs(5);
/// Bounds the response read; a status line plus axum's headers is far smaller.
const MAX_RESPONSE_BYTES: usize = 4 * 1024;
/// Bounds every readiness poll. The probe republishes every
/// `PROBE_INTERVAL`, so this allows several full rounds and then fails rather
/// than looping forever.
const SETTLE_ATTEMPTS: u32 = 200;
const SETTLE_INTERVAL: Duration = Duration::from_millis(50);
/// Reachability-probe cadence.
const PROBE_INTERVAL: Duration = Duration::from_secs(2);

/// The full readiness transition a readiness probe would observe.
///
/// 503 while no runtime-host socket exists -> 200 once the host is listening ->
/// 503 again the moment the host goes away. Before the reachability probe
/// existed the first assertion passed for the wrong reason and the second could
/// never be reached at all.
#[tokio::test]
async fn readyz_follows_runtime_host_reachability() {
    let socket = HostSocket::reserve("gateway-readiness-transition");
    let harness = Harness::start(&socket.path).await;

    assert_eq!(
        harness.readyz().await,
        503,
        "readiness must fail while no runtime host is listening"
    );

    let listener = UnixListener::bind(&socket.path).expect("bind runtime-host control socket");
    harness.await_readyz(200).await;

    drop(listener);
    // The listener is gone, so the path either no longer accepts or is a stale
    // socket file that refuses. Removing it covers both without depending on
    // which one the platform leaves behind.
    let _ = std::fs::remove_file(&socket.path);
    harness.await_readyz(503).await;

    harness.shutdown().await;
}

/// The property that matters most: an unreachable host never yields readiness.
///
/// An over-eager readiness signal is strictly worse than the original defect,
/// because it routes live traffic into a pod whose only output -- the
/// runtime-host endpoint returned by `/v1/runtime/resolve` -- is unusable.
#[tokio::test]
async fn readyz_stays_unready_while_the_runtime_host_is_absent() {
    let socket = HostSocket::reserve("gateway-readiness-absent");
    let harness = Harness::start(&socket.path).await;

    // Long enough for several probe rounds to have run and published.
    for _ in 0..8 {
        assert_eq!(
            harness.readyz().await,
            503,
            "readiness must not be asserted against an absent runtime host"
        );
        tokio::time::sleep(PROBE_INTERVAL / 4).await;
    }
    assert!(!harness.core.health_snapshot().ready());
    // Liveness is unaffected: the gateway process itself is healthy.
    assert_eq!(harness.readyz_path("/healthz").await, 200);

    harness.shutdown().await;
}

/// A gateway whose route snapshot names an endpoint the dispatch path could
/// never connect to is not ready either. The probe shares its endpoint parsing
/// with `connect_host`, so this cannot drift from what dispatch does.
#[tokio::test]
async fn readyz_stays_unready_for_an_endpoint_dispatch_could_not_use() {
    let harness = Harness::start_with_endpoint("http://runtime-host.invalid:9000").await;

    for _ in 0..4 {
        assert_eq!(
            harness.readyz().await,
            503,
            "a non-unix route endpoint is unusable by dispatch and must not read as ready"
        );
        tokio::time::sleep(PROBE_INTERVAL / 4).await;
    }

    harness.shutdown().await;
}

/// A runtime-gateway process wired exactly as `bootstrap::run` wires it.
struct Harness {
    core: Arc<GatewayCore>,
    address: SocketAddr,
    shutdown: watch::Sender<bool>,
    server: tokio::task::JoinHandle<Result<(), std::io::Error>>,
}

impl core::fmt::Debug for Harness {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("Harness")
            .field("address", &self.address)
            .finish_non_exhaustive()
    }
}

impl Harness {
    async fn start(socket: &Path) -> Self {
        Self::start_with_endpoint(&format!("unix://{}", socket.display())).await
    }

    async fn start_with_endpoint(endpoint: &str) -> Self {
        let policy = policy_cache(endpoint);
        let health = Arc::new(GatewayHealth::new());
        let core = Arc::new(
            GatewayCore::new(GatewayConfig::default(), policy.clone(), health.clone())
                .expect("gateway core"),
        );

        // Start through the real lifecycle so readiness inputs are asserted by
        // the component, never by the test.
        let mut service = Service::new();
        service
            .register(Box::new(GatewayComponent::new(
                core.clone(),
                health.clone(),
            )))
            .expect("register gateway component");
        service.start().expect("start gateway component");

        let address = reserve_loopback_address();
        let state = GatewayNetworkState::new(core.clone(), 64 * 1024, 64 * 1024, false)
            .expect("gateway network state");
        let (shutdown, shutdown_rx) = watch::channel(false);
        let server = tokio::spawn(network::serve(address, state, shutdown_rx));
        wait_until_serving(address).await;
        Self {
            core,
            address,
            shutdown,
            server,
        }
    }

    async fn readyz(&self) -> u16 {
        self.readyz_path("/readyz").await
    }

    async fn readyz_path(&self, path: &str) -> u16 {
        probe_status(self.address, path).await
    }

    /// Poll `/readyz` until it reports `expected`, within a hard bound.
    async fn await_readyz(&self, expected: u16) {
        let mut observed = 0;
        for _ in 0..SETTLE_ATTEMPTS {
            observed = self.readyz().await;
            if observed == expected {
                return;
            }
            tokio::time::sleep(SETTLE_INTERVAL).await;
        }
        panic!("/readyz never reached {expected}; last observed {observed}");
    }

    async fn shutdown(self) {
        self.shutdown.send(true).expect("request shutdown");
        self.server
            .await
            .expect("serve task")
            .expect("graceful gateway shutdown");
    }
}

/// A reserved, unused runtime-host socket path that is cleaned up on drop.
#[derive(Debug)]
struct HostSocket {
    path: PathBuf,
}

impl HostSocket {
    /// `DeploymentRoute` endpoints are bounded to a 100-byte socket path, which
    /// is also the platform's `sun_path` limit, so the shortest usable base
    /// directory is chosen rather than assumed.
    fn reserve(label: &str) -> Self {
        let unique = format!(
            "{}-{}-{}.sock",
            label,
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .expect("clock after epoch")
                .as_nanos()
        );
        let bases = [std::env::temp_dir(), PathBuf::from("/tmp")];
        let path = bases
            .into_iter()
            .map(|base| base.join(&unique))
            .find(|candidate| {
                candidate.is_absolute() && candidate.as_os_str().as_encoded_bytes().len() <= 100
            })
            .expect("a temporary directory short enough for a unix socket path");
        let _ = std::fs::remove_file(&path);
        Self { path }
    }
}

impl Drop for HostSocket {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.path);
    }
}

struct AcceptAll;

impl SignatureVerifier for AcceptAll {
    fn verify(&self, _payload: &[u8], _signature: &DetachedSignature) -> FaultResult<()> {
        Ok(())
    }
}

fn id(kind: &str, suffix: &str) -> ResourceId {
    format!("{kind}_01890f2c7b7a70008{suffix}")
        .parse()
        .expect("valid UUIDv7 resource id")
}

fn signature() -> DetachedSignature {
    DetachedSignature {
        algorithm: "test".into(),
        key_id: "test-key".into(),
        value: vec![1],
    }
}

/// A policy cache holding one installed, signed route snapshot that points at
/// `endpoint`. The snapshot validity window is far in the future so the probe
/// observes reachability rather than expiry.
fn policy_cache(endpoint: &str) -> Arc<PolicyCache> {
    let verifier: Arc<dyn SignatureVerifier> = Arc::new(AcceptAll);
    let now = now_unix_millis();
    let revocations = RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1,
            created_unix_millis: now - 1_000,
            expires_unix_millis: now + 3_600_000,
            revoked_grant_ids: BTreeSet::new(),
            revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(),
            revoked_bundle_digests: BTreeSet::new(),
        },
        signature: signature(),
    };
    let cache = Arc::new(PolicyCache::new(verifier, revocations, now).expect("policy cache"));
    let mut claims = RouteSnapshotClaims {
        snapshot_id: id("routesnap", "000000000000001"),
        snapshot_digest: Digest::ZERO,
        version: 1,
        policy_epoch: 1,
        revocation_epoch: 1,
        created_unix_millis: now - 1_000,
        expires_unix_millis: now + 3_600_000,
        routes: vec![DeploymentRoute {
            deployment_id: id("deployment", "000000000000000"),
            model_bundle: hash_bytes(b"model"),
            engine_bundle: hash_bytes(b"engine"),
            endpoint: endpoint.to_owned(),
            region: "us-central1".into(),
            weight: 100,
            capabilities: BTreeSet::from(["structure".into()]),
            lease_expires_unix_millis: now + 3_600_000,
            safety_policy: None,
        }],
        minimum_runtime_version: "1".into(),
    };
    claims.snapshot_digest = claims.computed_digest().expect("snapshot digest");
    cache
        .install_route(
            RouteSnapshot {
                claims,
                signature: signature(),
            },
            now,
        )
        .expect("route install");
    cache
}

fn now_unix_millis() -> u64 {
    u64::try_from(
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .expect("clock after epoch")
            .as_millis(),
    )
    .expect("clock within u64 milliseconds")
}

/// Take a loopback port from the kernel and release it immediately.
///
/// `network::serve` binds the address itself, so the port has to be chosen
/// before the call rather than read back from the listener.
fn reserve_loopback_address() -> SocketAddr {
    let listener = std::net::TcpListener::bind("127.0.0.1:0").expect("reserve loopback port");
    let address = listener.local_addr().expect("loopback address");
    drop(listener);
    address
}

/// Wait for the listener to accept connections. Readiness is deliberately NOT
/// part of this condition: these tests are about when readiness flips.
async fn wait_until_serving(address: SocketAddr) {
    for _ in 0..SETTLE_ATTEMPTS {
        if TcpStream::connect(address).await.is_ok() {
            return;
        }
        tokio::time::sleep(SETTLE_INTERVAL).await;
    }
    panic!("runtime gateway never began serving on {address}");
}

/// Issue one bounded HTTP/1.1 request and return the response status code.
///
/// Deliberately hand-rolled: the assertion is about what a readiness probe on
/// the wire observes, so it must not route through anything that could hold a
/// pooled connection or retry.
async fn probe_status(address: SocketAddr, path: &str) -> u16 {
    let request = format!("GET {path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n");
    let response = timeout(PROBE_TIMEOUT, async {
        let mut stream = TcpStream::connect(address).await.expect("probe connect");
        stream
            .write_all(request.as_bytes())
            .await
            .expect("probe write");
        stream.flush().await.expect("probe flush");
        let mut buffer = vec![0_u8; MAX_RESPONSE_BYTES];
        let mut filled = 0;
        while filled < buffer.len() {
            let read = stream
                .read(&mut buffer[filled..])
                .await
                .expect("probe read");
            if read == 0 {
                break;
            }
            filled += read;
        }
        buffer.truncate(filled);
        buffer
    })
    .await
    .expect("probe did not complete within its bound");

    let text = String::from_utf8_lossy(&response);
    let status = text
        .split_whitespace()
        .nth(1)
        .expect("HTTP status line has a code");
    status.parse().expect("HTTP status code is numeric")
}
