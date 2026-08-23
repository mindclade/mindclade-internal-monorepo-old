// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded periodic reachability probe for the node-local runtime host.
//!
//! # What this asserts, precisely
//!
//! This probe establishes that the runtime-host control socket named by the
//! currently installed signed route snapshot **accepts a connection**. It does
//! NOT establish that the host considers itself ready: `WorkerControl` exposes
//! only `rpc Execute` (`protocols/proto/mindclade/runtime/v1/service.proto`),
//! so there is no host health RPC to call, and inventing one would change a
//! contract this service does not own. Every name and message below says
//! "reachable" for that reason. `GatewayHealth`'s flag keeps its established
//! spelling because the runbook and SLO documents cite it, but its meaning is
//! exactly the one stated here.
//!
//! # Why reachability is the right readiness input
//!
//! The gateway's entire output is a runtime-host endpoint. `/v1/runtime/resolve`
//! is always mounted and returns `endpoint` -- the host's Unix-domain socket --
//! as its whole payload, so this holds even when `execution_enabled` is false.
//! The topology is node-local: endpoints are `unix://` paths and `runtime_host`
//! serves on `MINDCLADE_RUNTIME_HOST_GRPC_SOCKET`. A gateway that cannot reach
//! its host is therefore a gateway whose answers are unusable, and reporting
//! unready sheds that traffic to a node that can serve it.
//!
//! # Why *every* endpoint must be reachable
//!
//! Route selection is deterministic and weighted over the signed snapshot; it
//! cannot exclude a dead endpoint on its own. `docs/runbooks/runtime-gateway-degraded.md`
//! records the remedy for a host fault as removing the host from local routing,
//! so a snapshot that still lists an unreachable endpoint is by definition a
//! degraded gateway: some fraction of admitted requests will be steered at a
//! host that is not there.
//!
//! # Fail-closed
//!
//! Every failure path -- a clock fault, an absent or expired snapshot, an
//! endpoint the dispatch path could not parse, too many distinct endpoints to
//! probe within the round budget, a refused or timed-out connect -- publishes
//! `false`. The defect this replaces was safe but useless; an over-eager
//! readiness signal would route live traffic into a broken pod, which is
//! strictly worse.

use crate::GatewayHealth;
use crate::PolicyCache;
use crate::network::unix_millis;
use mindclade_faults::FaultResult;
use std::collections::BTreeSet;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use tokio::net::UnixStream;
use tokio::sync::watch;
use tokio::time::{Instant, timeout};

/// Cadence between probe rounds. Chosen well below a conventional 10s readiness
/// probe period so an orchestrator observes a transition within one of its own
/// periods, and far above the cost of a Unix-socket connect.
pub const PROBE_INTERVAL: Duration = Duration::from_secs(2);

/// Per-endpoint connect budget. A Unix-socket connect normally resolves in
/// microseconds; this only bites when the host's listen backlog is saturated,
/// which is itself a reason to report unready.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(1);

/// Whole-round budget. Bounds the round independently of the endpoint count so
/// a slow round cannot stretch without limit, and so the loop's period stays
/// predictable. Exceeding it publishes `false`.
const ROUND_DEADLINE: Duration = Duration::from_secs(5);

/// Distinct-endpoint cap for one round. The topology is node-local, so a real
/// snapshot names one or two sockets; `RouteSnapshotClaims` nevertheless admits
/// up to 16384 routes, and probing an unbounded set on a timer is exactly the
/// unbounded work this repository forbids. A snapshot past this cap is reported
/// unready rather than partially probed, because a partial probe would assert
/// readiness over endpoints nothing checked.
const MAX_PROBE_ENDPOINTS: usize = 16;

/// Unix `sun_path` bound, mirrored from `connect_host`'s own endpoint check.
const MAX_SOCKET_PATH_BYTES: usize = 100;

/// The socket path a `unix://` route endpoint denotes, or `None` when the
/// dispatch path could not use it.
///
/// Shared with `grpc::connect_host` deliberately: if the probe accepted an
/// endpoint form that dispatch rejects, or vice versa, readiness would stop
/// describing the connection the gateway actually makes.
#[must_use]
pub fn host_socket_path(endpoint: &str) -> Option<PathBuf> {
    endpoint
        .strip_prefix("unix://")
        .map(PathBuf::from)
        .filter(|path| {
            path.is_absolute() && path.as_os_str().as_encoded_bytes().len() <= MAX_SOCKET_PATH_BYTES
        })
}

/// Publish runtime-host reachability until `shutdown` is asserted.
///
/// Runs as one arm of the bootstrap `try_join!` rather than a detached task, so
/// it shares the process runtime, is cancelled by the same `watch` channel as
/// the serve loops, and cannot outlive them. Every wait inside is deadlined.
pub async fn run(
    policy: Arc<PolicyCache>,
    health: Arc<GatewayHealth>,
    mut shutdown: watch::Receiver<bool>,
) -> FaultResult<()> {
    let mut published: Option<bool> = None;
    while !*shutdown.borrow() {
        let reachable = probe_round(policy.as_ref()).await;
        publish(health.as_ref(), reachable, &mut published);
        tokio::select! {
            changed = shutdown.changed() => {
                if changed.is_err() {
                    break;
                }
            }
            () = tokio::time::sleep(PROBE_INTERVAL) => {}
        }
    }
    // Shutting down is not a reachability observation, but it is unambiguously
    // not a readiness claim either. Drain already clears `accepting`; clearing
    // this too means no path leaves a stale `true` behind for a restart to
    // inherit through shared state.
    publish(health.as_ref(), false, &mut published);
    Ok(())
}

/// One bounded round: true only when every distinct endpoint in the installed
/// snapshot accepted a connection within the round budget.
async fn probe_round(policy: &PolicyCache) -> bool {
    let Ok(now) = unix_millis() else {
        return false;
    };
    // A snapshot that will not validate is also a snapshot whose endpoints
    // carry no authority, so there is nothing meaningful to probe.
    let Ok(snapshot) = policy.snapshot(now) else {
        return false;
    };
    let endpoints: BTreeSet<&str> = snapshot
        .route
        .claims
        .routes
        .iter()
        .map(|route| route.endpoint.as_str())
        .collect();
    if endpoints.is_empty() || endpoints.len() > MAX_PROBE_ENDPOINTS {
        return false;
    }
    let Some(deadline) = Instant::now().checked_add(ROUND_DEADLINE) else {
        return false;
    };
    for endpoint in endpoints {
        if !probe_endpoint(endpoint, deadline).await {
            return false;
        }
    }
    true
}

/// Connect once to `endpoint`, bounded by both the per-endpoint budget and the
/// remaining round budget, and drop the connection immediately.
///
/// The connection is opened and closed rather than pooled: this is a liveness
/// question about the socket, and holding an idle control channel open per
/// round would be an unbounded resource the probe does not need.
async fn probe_endpoint(endpoint: &str, deadline: Instant) -> bool {
    let Some(path) = host_socket_path(endpoint) else {
        return false;
    };
    let remaining = deadline.saturating_duration_since(Instant::now());
    if remaining.is_zero() {
        return false;
    }
    match timeout(CONNECT_TIMEOUT.min(remaining), UnixStream::connect(&path)).await {
        Ok(Ok(stream)) => {
            drop(stream);
            true
        }
        Ok(Err(_)) | Err(_) => false,
    }
}

/// Store the observation, announcing only transitions.
///
/// Transition-only on purpose: this process has no tracing or logging crate, so
/// the alternative idiom is `main.rs`'s `eprintln!`, and a per-round line would
/// be unbounded output for a host that stays down. The store happens on every
/// round regardless, so the published flag never depends on the log.
fn publish(health: &GatewayHealth, reachable: bool, published: &mut Option<bool>) {
    if *published != Some(reachable) {
        let transition = if reachable {
            "reachable"
        } else {
            "unreachable"
        };
        eprintln!("mindclade-runtime-gateway: runtime-host control socket is {transition}");
        *published = Some(reachable);
    }
    health.set_runtime_host_ready(reachable);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_absolute_bounded_unix_endpoints_are_probeable() {
        assert_eq!(
            host_socket_path("unix:///run/mindclade/runtime-host.sock"),
            Some(PathBuf::from("/run/mindclade/runtime-host.sock"))
        );
        // Exactly what `connect_host` refuses, and for the same reasons.
        assert_eq!(host_socket_path("http://runtime-host:9000"), None);
        assert_eq!(host_socket_path("unix://relative/path.sock"), None);
        assert_eq!(host_socket_path(""), None);
        let overlong = format!("unix:///{}", "a".repeat(MAX_SOCKET_PATH_BYTES));
        assert_eq!(host_socket_path(&overlong), None);
    }

    #[tokio::test]
    async fn an_expired_round_budget_reports_unreachable_without_connecting() {
        let deadline = Instant::now();
        tokio::time::sleep(Duration::from_millis(1)).await;
        assert!(!probe_endpoint("unix:///run/mindclade/runtime-host.sock", deadline).await);
    }

    #[tokio::test]
    async fn an_absent_socket_reports_unreachable() {
        let deadline = Instant::now()
            .checked_add(ROUND_DEADLINE)
            .expect("probe deadline within clock range");
        assert!(!probe_endpoint("unix:///nonexistent/mindclade-runtime-host.sock", deadline).await);
    }

    #[test]
    fn publishing_announces_transitions_and_always_stores() {
        let health = GatewayHealth::new();
        let mut published = None;
        publish(&health, false, &mut published);
        assert_eq!(published, Some(false));
        assert!(!health.snapshot().runtime_host_ready);
        publish(&health, true, &mut published);
        assert_eq!(published, Some(true));
        assert!(health.snapshot().runtime_host_ready);
    }
}
