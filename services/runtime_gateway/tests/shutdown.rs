// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::FaultResult;
use mindclade_runtime_gateway::bootstrap::drain_on_termination;
use mindclade_runtime_gateway::network::{self, GatewayNetworkState};
use mindclade_runtime_gateway::{
    GatewayComponent, GatewayConfig, GatewayCore, GatewayHealth, PolicyCache,
};
use mindclade_servicekit::Service;
use mindclade_worker_protocol::{
    DetachedSignature, RevocationSnapshot, RevocationSnapshotClaims, SignatureVerifier,
};
use std::collections::BTreeSet;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::{oneshot, watch};
use tokio::time::timeout;

/// Bounds every network wait in this file. A probe that hangs is a failure, not
/// a reason for the suite to stall until the harness kills it.
const PROBE_TIMEOUT: Duration = Duration::from_secs(5);
/// Bounds the readiness poll: 100 attempts at 20ms is two seconds of listener
/// startup, after which the test fails rather than looping forever.
const READY_ATTEMPTS: u32 = 100;
const READY_INTERVAL: Duration = Duration::from_millis(20);
/// Bounds the response read. A status line plus the headers axum emits is well
/// under this; anything larger is truncated rather than buffered without limit.
const MAX_RESPONSE_BYTES: usize = 4 * 1024;

#[test]
fn drain_removes_readiness_before_process_shutdown() {
    let health = GatewayHealth::new();
    health.set_accepting(true);
    health.set_policy_fresh(true);
    health.set_runtime_host_ready(true);
    assert!(health.snapshot().ready());
    health.set_accepting(false);
    assert!(!health.snapshot().ready());
    assert!(health.snapshot().live());
}

/// Observe `/readyz` DURING the shutdown window rather than after it.
///
/// The regression this pins: the gateway used to drain only in the reverse
/// `service.stop()` pass, which runs after `try_join!` returns. The serve loops
/// were told to unwind while the process was still advertising itself ready, so
/// for the whole graceful-drain interval a load balancer kept routing new work
/// to a gateway that was tearing down.
///
/// The test stands exactly where the serve loops stand -- it waits on the same
/// `watch` channel they wait on -- and probes the real HTTP `/readyz` route at
/// the instant that channel fires, while the listener is still fully live. That
/// is the moment a readiness probe would answer during a real drain. Releasing
/// the serve loop is deliberately deferred until after the probe so the
/// assertion cannot be satisfied by the listener simply having gone away.
#[tokio::test]
async fn readyz_reports_unready_before_the_serve_loops_unwind() {
    let health = Arc::new(GatewayHealth::new());
    let core = Arc::new(
        GatewayCore::new(GatewayConfig::default(), policy_cache(), health.clone())
            .expect("gateway core"),
    );

    // Start through the real lifecycle, exactly as bootstrap does, so readiness
    // is asserted by the component rather than by the test.
    let mut service = Service::new();
    service
        .register(Box::new(GatewayComponent::new(
            core.clone(),
            health.clone(),
        )))
        .expect("register gateway component");
    service.start().expect("start gateway component");
    // The only readiness input the component does not own. Nothing in the
    // production bootstrap sets it either -- see the note in the pull request.
    health.set_runtime_host_ready(true);
    assert!(core.health_snapshot().ready());

    let address = reserve_loopback_address();
    let state = GatewayNetworkState::new(core.clone(), 64 * 1024, 64 * 1024, false)
        .expect("gateway network state");

    // Two channels on purpose. `shutdown_tx` is the one the process wires to
    // the signal handler; `serve_tx` stands in for the serve loops so the test
    // can hold them open across the probe.
    let (shutdown_tx, mut shutdown_rx) = watch::channel(false);
    let (serve_tx, serve_rx) = watch::channel(false);
    let server = tokio::spawn(network::serve(address, state, serve_rx));
    wait_until_ready(address).await;

    let (termination_tx, termination_rx) = oneshot::channel::<()>();
    let signal = tokio::spawn(drain_on_termination(
        core.clone(),
        shutdown_tx,
        async move {
            let _ = termination_rx.await;
        },
    ));
    termination_tx.send(()).expect("request termination");

    // Wait where the serve loops wait: the instant the shutdown signal is
    // observable is the instant they would begin unwinding.
    shutdown_rx.changed().await.expect("shutdown signal");
    assert!(*shutdown_rx.borrow(), "shutdown signal must be asserted");

    // The serve loops have NOT been released yet, so this is a live probe
    // against a still-serving listener -- the readiness a load balancer would
    // see at the top of the drain window.
    assert_eq!(
        probe_status(address, "/readyz").await,
        503,
        "readiness must fail before the serve loops are told to unwind"
    );
    assert!(!core.health_snapshot().ready());
    // Liveness is unaffected: the process is draining, not dead.
    assert_eq!(probe_status(address, "/healthz").await, 200);

    serve_tx.send(true).expect("release the serve loop");
    server
        .await
        .expect("serve task")
        .expect("graceful gateway shutdown");
    signal.await.expect("termination task");
}

struct AcceptAll;
impl SignatureVerifier for AcceptAll {
    fn verify(&self, _payload: &[u8], _signature: &DetachedSignature) -> FaultResult<()> {
        Ok(())
    }
}

fn policy_cache() -> Arc<PolicyCache> {
    let verifier: Arc<dyn SignatureVerifier> = Arc::new(AcceptAll);
    let revocations = RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1,
            created_unix_millis: 100,
            expires_unix_millis: 10_000,
            revoked_grant_ids: BTreeSet::new(),
            revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(),
            revoked_bundle_digests: BTreeSet::new(),
        },
        signature: DetachedSignature {
            algorithm: "test".into(),
            key_id: "test-key".into(),
            value: vec![1],
        },
    };
    Arc::new(PolicyCache::new(verifier, revocations, 200).expect("policy cache"))
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

async fn wait_until_ready(address: SocketAddr) {
    for _ in 0..READY_ATTEMPTS {
        if TcpStream::connect(address).await.is_ok()
            && probe_status(address, "/readyz").await == 200
        {
            return;
        }
        tokio::time::sleep(READY_INTERVAL).await;
    }
    panic!("runtime gateway never became ready on {address}");
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
