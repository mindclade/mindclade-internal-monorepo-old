// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded operational HTTP plane: liveness, readiness, and proxy counters.
//!
//! This is the surface an orchestrator and a load balancer consume, and it is
//! the only listener this process opens. It carries no artifact bytes: the
//! tenant-scoped byte plane has no wire contract in `protocols/` — the
//! `ArtifactService` there is the control plane's catalog, which this service
//! explicitly does not own — and inventing a byte-plane API here would be a
//! protocol decision taken inside a service. See the README's "What this
//! process serves" section; readiness is scoped to exactly what is listed
//! there and to nothing beyond it.

use crate::dependencies::ObjectStoreProbe;
use crate::{ArtifactProxyCore, ProxyHealthSnapshot, ProxyMetrics};
use axum::Router;
use axum::extract::{DefaultBodyLimit, State};
use axum::http::StatusCode;
use axum::routing::get;
use mindclade_servicekit::{HealthRegistry, HealthStatus, Lifecycle};
use std::fmt::Write as _;
use std::future::IntoFuture;
use std::io;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;
use tokio::net::TcpListener;
use tokio::sync::{oneshot, watch};
use tokio::time::timeout;
use tower::limit::ConcurrencyLimitLayer;

/// Registry key under which the core publishes its own state.
pub const CORE_REPORT: &str = "artifact-proxy-core";
/// Registry key under which the backing object store publishes.
pub const OBJECT_STORE_REPORT: &str = "artifact-proxy-object-store";

/// Probes are metadata reads, not transfers; a second is already generous.
const PROBE_BUDGET: Duration = Duration::from_secs(1);
/// Established operational requests get this long once drain starts.
const OPERATIONS_DRAIN_TIMEOUT: Duration = Duration::from_secs(5);
const MAX_OPERATIONS_CONCURRENCY: usize = 256;
/// The operational plane reads no request bodies. The limit exists so a body
/// that arrives anyway is refused at the framing layer rather than buffered.
const MAX_OPERATIONS_BODY_BYTES: usize = 1024;

#[derive(Clone, Debug)]
pub struct OperationsState {
    core: Arc<ArtifactProxyCore>,
    probe: ObjectStoreProbe,
    reports: HealthRegistry,
    lifecycle: Lifecycle,
    metrics: ProxyMetrics,
}

impl OperationsState {
    #[must_use]
    pub fn new(
        core: Arc<ArtifactProxyCore>,
        probe: ObjectStoreProbe,
        reports: HealthRegistry,
        lifecycle: Lifecycle,
        metrics: ProxyMetrics,
    ) -> Self {
        Self {
            core,
            probe,
            reports,
            lifecycle,
            metrics,
        }
    }

    /// Republish the core's own state into the registry before answering.
    ///
    /// The accounting latch is why this exists. `ProxyHealth` clears
    /// `accounting_healthy` on a transfer-counter under/overflow and nothing in
    /// the crate ever restores it, so from that moment the process refuses
    /// every transfer for the rest of its life. Left unpublished that state is
    /// invisible — the process keeps answering probes and silently admits
    /// nothing. Publishing it as `Unhealthy` makes it fail liveness, which is
    /// what turns a silent permanent wedge into a restart the platform performs
    /// on its own. Restart is the recovery; there is deliberately no in-process
    /// reset, because a counter that has already gone negative cannot be
    /// reconciled with the transfers it was counting.
    fn publish_core_report(&self) -> ProxyHealthSnapshot {
        let snapshot = self.core.health_snapshot();
        let (status, message) = if snapshot.accounting_healthy {
            (HealthStatus::Healthy, "transfer accounting is consistent")
        } else {
            (
                HealthStatus::Unhealthy,
                "transfer accounting latched corrupt; this process can never admit work again and must be restarted",
            )
        };
        let _ = self.reports.set(CORE_REPORT, status, message);
        snapshot
    }

    async fn publish_store_report(&self) {
        let (status, message) = match self.probe.check(PROBE_BUDGET).await {
            Ok(()) => (
                HealthStatus::Healthy,
                String::from("artifact object store answered a metadata read"),
            ),
            Err(error) => (
                HealthStatus::Unhealthy,
                format!(
                    "artifact object store is unreachable: {}: {}",
                    error.code().as_str(),
                    error.message()
                ),
            ),
        };
        let _ = self.reports.set(OBJECT_STORE_REPORT, status, message);
    }

    /// `mindclade_servicekit::server::live`, evaluated over the shareable
    /// `Lifecycle` handle because `Service` owns non-`Sync` components and
    /// cannot be handed to a request handler. Both halves are required.
    fn is_live(&self) -> bool {
        self.lifecycle.state().is_live() && self.reports.is_live()
    }

    /// `mindclade_servicekit::server::ready`, evaluated the same way.
    ///
    /// The registry half is empty until the lifecycle's start pass files the
    /// first report, and an empty registry is not ready — so this cannot answer
    /// ready before the process has actually started something.
    fn is_ready(&self) -> bool {
        self.lifecycle.state().admits_traffic() && self.reports.is_ready()
    }

    fn render(&self) -> String {
        let mut body = format!("phase={}\n", self.lifecycle.state());
        for (component, report) in self.reports.snapshot() {
            let _ = writeln!(
                body,
                "component={component} status={} message={}",
                status_name(report.status),
                report.message
            );
        }
        body
    }
}

const fn status_name(status: HealthStatus) -> &'static str {
    match status {
        HealthStatus::Healthy => "healthy",
        HealthStatus::Degraded => "degraded",
        HealthStatus::Unhealthy => "unhealthy",
        HealthStatus::Starting => "starting",
    }
}

/// Serve the operational plane on an already-bound listener until `shutdown`.
///
/// Taking the listener rather than an address is what lets the composition root
/// bind before it starts the lifecycle — and lets a test bind port 0 and learn
/// the address. Once `shutdown` is asserted the listener stops accepting and
/// established requests get `OPERATIONS_DRAIN_TIMEOUT` to finish; exceeding
/// that fails the server rather than leaving a connection tree with no owner.
pub async fn serve(
    listener: TcpListener,
    state: OperationsState,
    mut shutdown: watch::Receiver<bool>,
) -> Result<(), io::Error> {
    let already_shutting_down = *shutdown.borrow_and_update();
    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/readyz", get(readyz))
        .route("/metrics", get(metrics))
        .layer(DefaultBodyLimit::max(MAX_OPERATIONS_BODY_BYTES))
        .layer(ConcurrencyLimitLayer::new(MAX_OPERATIONS_CONCURRENCY))
        .with_state(state);

    let (drain_tx, drain_rx) = oneshot::channel::<()>();
    let mut drain_tx = Some(drain_tx);
    let server = axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            let _ = drain_rx.await;
        })
        .into_future();
    tokio::pin!(server);

    if already_shutting_down {
        if let Some(sender) = drain_tx.take() {
            let _ = sender.send(());
        }
    } else {
        loop {
            tokio::select! {
                result = &mut server => return result,
                changed = shutdown.changed() => {
                    if changed.is_err() || *shutdown.borrow() {
                        if let Some(sender) = drain_tx.take() {
                            let _ = sender.send(());
                        }
                        break;
                    }
                }
            }
        }
    }

    match timeout(OPERATIONS_DRAIN_TIMEOUT, &mut server).await {
        Ok(result) => result,
        Err(_) => Err(io::Error::new(
            io::ErrorKind::TimedOut,
            "artifact proxy operational plane exceeded its graceful-drain deadline",
        )),
    }
}

/// Bind the operational address, failing closed when it cannot be taken.
pub async fn bind(address: SocketAddr) -> Result<TcpListener, io::Error> {
    TcpListener::bind(address).await
}

async fn healthz(State(state): State<OperationsState>) -> (StatusCode, String) {
    state.publish_core_report();
    let status = if state.is_live() {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    };
    (status, state.render())
}

async fn readyz(State(state): State<OperationsState>) -> (StatusCode, String) {
    state.publish_core_report();
    state.publish_store_report().await;
    let status = if state.is_ready() {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    };
    (status, state.render())
}

async fn metrics(State(state): State<OperationsState>) -> (StatusCode, String) {
    let snapshot = state.publish_core_report();
    let mut body = String::new();
    for (name, value) in state.metrics.snapshot() {
        let _ = writeln!(body, "{} {value}", name.replace('.', "_"));
    }
    let _ = writeln!(
        body,
        "artifact_proxy_active_transfers {}\nartifact_proxy_accepting {}\nartifact_proxy_accounting_healthy {}",
        snapshot.active_transfers,
        u8::from(snapshot.accepting),
        u8::from(snapshot.accounting_healthy),
    );
    (StatusCode::OK, body)
}
