// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Fail-closed artifact-proxy process bootstrap.
//!
//! ADR-0010 puts provider construction, resource configuration, and lifecycle
//! wiring inside `services/`, and `docs/architecture/service-boundaries.md`
//! spells out the Rust composition path this module walks. `runtime_gateway`
//! and `runtime_host` walk the same one.
//!
//! The prior entry point called itself "deliberately a fail-closed composition
//! seam" and then exited 0, which is the opposite of failing closed: to an
//! orchestrator a zero exit is a process that ran and succeeded. Nothing in the
//! repository recorded a decision that composition lived outside it — ADR-0010
//! says the reverse, and `PRODUCTION_READINESS.md` still lists the composed
//! listener as required-and-missing evidence rather than as out of scope.
//!
//! Every input is required and bounded. Nothing defaults: a byte plane that is
//! not told its limits must not invent them.

use crate::dependencies::ObjectStoreProbe;
use crate::lifecycle::ObjectStoreComponent;
use crate::operations::{self, OperationsState};
use crate::{
    ArtifactProxyComponent, ArtifactProxyCore, LocalCache, ProxyConfig, ProxyHealth, ProxyMetrics,
    TransferEngine,
};
use mindclade_artifact_cas::{ArtifactCas, CasConfig};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_object_store::{LocalStore, ObjectStore};
use mindclade_runtime_core::SystemClock;
use mindclade_servicekit::{HealthRegistry, Service, ServiceConfig, signals};
use std::env;
use std::future::Future;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::watch;

const SERVICE_NAME: &str = "artifact-proxy";
const MAX_ENV_VALUE_BYTES: usize = 4096;
/// Startup probe budget. Larger than the readiness budget on purpose: a cold
/// provider is allowed to be slow once, but not on every probe thereafter.
const STARTUP_PROBE_BUDGET: Duration = Duration::from_secs(5);

#[derive(Clone, Debug)]
pub struct BootstrapConfig {
    pub operations_address: SocketAddr,
    pub object_store_root: PathBuf,
    pub proxy: ProxyConfig,
}

impl BootstrapConfig {
    pub fn from_env() -> FaultResult<Self> {
        let operations_address = required("MINDCLADE_ARTIFACT_PROXY_OPERATIONS_ADDR")?
            .parse::<SocketAddr>()
            .map_err(|error| {
                Fault::invalid_argument("artifact proxy operations address is invalid")
                    .with_source(error)
            })?;
        let object_store_root = absolute_path("MINDCLADE_ARTIFACT_PROXY_STORE_ROOT")?;
        let proxy = ProxyConfig {
            maximum_read_bytes: parse_u64("MINDCLADE_ARTIFACT_PROXY_MAX_READ_BYTES")?,
            maximum_write_bytes: parse_u64("MINDCLADE_ARTIFACT_PROXY_MAX_WRITE_BYTES")?,
            maximum_range_bytes: parse_u64("MINDCLADE_ARTIFACT_PROXY_MAX_RANGE_BYTES")?,
            maximum_cache_bytes: parse_u64("MINDCLADE_ARTIFACT_PROXY_MAX_CACHE_BYTES")?,
            maximum_cache_entries: parse_usize("MINDCLADE_ARTIFACT_PROXY_MAX_CACHE_ENTRIES")?,
            maximum_concurrent_transfers: parse_u32(
                "MINDCLADE_ARTIFACT_PROXY_MAX_CONCURRENT_TRANSFERS",
            )?,
        };
        proxy.validate()?;
        Ok(Self {
            operations_address,
            object_store_root,
            proxy,
        })
    }
}

/// Compose and run the artifact proxy until the operating system asks it to stop.
pub async fn run(config: BootstrapConfig) -> FaultResult<()> {
    run_until(config, signals::termination_requested(), |_| {}).await
}

/// `run`, with the termination source and the bound-address observer injected.
///
/// `bound` is called once with the address the operational listener actually
/// took, which is the only way to learn it when the configuration asks for port
/// 0. Kept public so a conformance test drives the same code path the signal
/// handler drives, rather than a parallel one that only resembles it.
pub async fn run_until<T, B>(config: BootstrapConfig, termination: T, bound: B) -> FaultResult<()>
where
    T: Future<Output = ()> + Send,
    B: FnOnce(SocketAddr) + Send,
{
    // Provider construction is the composition root's own job (ADR-0010), and
    // it happens here rather than in `run_with_store` so the assembly below can
    // be exercised against a provider that fails on demand.
    let store = Arc::new(LocalStore::new(&config.object_store_root)?);
    run_with_store(config, store, termination, bound).await
}

/// `run_until` against an already-constructed object-store provider.
pub async fn run_with_store<T, B>(
    config: BootstrapConfig,
    store: Arc<dyn ObjectStore>,
    termination: T,
    bound: B,
) -> FaultResult<()>
where
    T: Future<Output = ()> + Send,
    B: FnOnce(SocketAddr) + Send,
{
    let clock = Arc::new(SystemClock);
    let probe = ObjectStoreProbe::new(store.clone())?;

    // Fail closed before anything is registered. A byte plane whose object
    // store does not answer has nothing to serve, and coming up anyway puts a
    // backend into rotation whose first real request is its first discovery
    // that the store is gone.
    probe.check(STARTUP_PROBE_BUDGET).await?;

    // Bind before starting the lifecycle: an address already in use must fail
    // the process, not leave a started service with no way to be probed.
    let listener = operations::bind(config.operations_address)
        .await
        .map_err(|error| {
            Fault::new(Code::Unavailable, "artifact proxy operations bind failed")
                .with_source(error)
        })?;
    bound(listener.local_addr().map_err(|error| {
        Fault::new(
            Code::Unavailable,
            "artifact proxy operations address is unreadable",
        )
        .with_source(error)
    })?);

    let cas = ArtifactCas::new(store, clock.clone(), CasConfig::default())?;
    let cache = Arc::new(LocalCache::new(
        config.proxy.maximum_cache_bytes,
        config.proxy.maximum_cache_entries,
    )?);
    let metrics = ProxyMetrics::default();
    let transfer = TransferEngine::new(
        cas,
        cache,
        metrics.clone(),
        config.proxy.maximum_range_bytes,
    );
    let health = Arc::new(ProxyHealth::new());
    let core = Arc::new(ArtifactProxyCore::new(
        config.proxy,
        transfer,
        health,
        metrics.clone(),
    )?);
    let reports = HealthRegistry::new(clock.clone());

    let mut service = Service::with_config(ServiceConfig::new(SERVICE_NAME)?, clock)?;
    service.register(Box::new(ObjectStoreComponent::new(
        probe.clone(),
        reports.clone(),
    )))?;
    service.register(Box::new(ArtifactProxyComponent::new(
        core.clone(),
        reports.clone(),
    )))?;
    // Readiness is false until this returns: the registry is empty before the
    // start pass files a report, and `HealthRegistry::is_ready` treats an empty
    // registry as not ready.
    service.start()?;

    let state = OperationsState::new(core, probe, reports, service.lifecycle(), metrics);
    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let serve_result = serve_until_terminated(
        &mut service,
        operations::serve(listener, state, shutdown_rx),
        shutdown_tx,
        termination,
    )
    .await;

    // The serve error, if any, outranks a shutdown fault: it is the reason the
    // process is ending.
    let shutdown = service.stop();
    serve_result.and(shutdown)
}

/// Run the operational server, draining the moment termination is requested.
///
/// Ordering is the whole point. `Service::stop` drains too, but only after the
/// serve future has unwound — for that entire window `/readyz` would still
/// answer 200 and the load balancer would keep the backend in rotation. Calling
/// `Service::drain` at signal time moves the lifecycle to `draining`, which
/// `LifecycleState::admits_traffic` reports as false, so readiness drops before
/// the listener is told to stop accepting. `runtime_gateway` fixed the same
/// ordering defect; this is that shape, kept in one task so no lifecycle state
/// is mutated from a second owner.
async fn serve_until_terminated<S, T>(
    service: &mut Service,
    server: S,
    shutdown: watch::Sender<bool>,
    termination: T,
) -> FaultResult<()>
where
    S: Future<Output = Result<(), std::io::Error>>,
    T: Future<Output = ()> + Send,
{
    let mut server = std::pin::pin!(server);
    let mut termination = std::pin::pin!(termination);
    let mut drained = false;
    let mut drain_fault = None;
    let serve_result = loop {
        tokio::select! {
            result = &mut server => break result,
            () = &mut termination, if !drained => {
                drained = true;
                drain_fault = service.drain().err();
                let _ = shutdown.send(true);
            }
        }
    };
    serve_result.map_err(|error| {
        Fault::new(Code::Unavailable, "artifact proxy operations server failed").with_source(error)
    })?;
    drain_fault.map_or(Ok(()), Err)
}

fn required(name: &'static str) -> FaultResult<String> {
    let value = env::var(name).map_err(|_| {
        Fault::new(
            Code::FailedPrecondition,
            "required artifact-proxy environment variable is missing",
        )
        .with_context("variable", name)
    })?;
    if value.is_empty() || value != value.trim() || value.len() > MAX_ENV_VALUE_BYTES {
        return Err(
            Fault::invalid_argument("artifact-proxy environment value is invalid")
                .with_context("variable", name),
        );
    }
    Ok(value)
}

fn parse_u64(name: &'static str) -> FaultResult<u64> {
    required(name)?.parse::<u64>().map_err(|error| {
        Fault::invalid_argument("artifact-proxy integer environment value is invalid")
            .with_context("variable", name)
            .with_source(error)
    })
}

fn parse_u32(name: &'static str) -> FaultResult<u32> {
    required(name)?.parse::<u32>().map_err(|error| {
        Fault::invalid_argument("artifact-proxy integer environment value is invalid")
            .with_context("variable", name)
            .with_source(error)
    })
}

fn parse_usize(name: &'static str) -> FaultResult<usize> {
    required(name)?.parse::<usize>().map_err(|error| {
        Fault::invalid_argument("artifact-proxy integer environment value is invalid")
            .with_context("variable", name)
            .with_source(error)
    })
}

fn absolute_path(name: &'static str) -> FaultResult<PathBuf> {
    let path = PathBuf::from(required(name)?);
    if !path.is_absolute() {
        return Err(
            Fault::invalid_argument("artifact-proxy path must be absolute")
                .with_context("variable", name),
        );
    }
    Ok(path)
}
