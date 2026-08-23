// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Process-level conformance for the composed artifact-proxy binary.
//!
//! These tests start the real composition root — the same `bootstrap` entry the
//! binary calls — bind a real socket, and speak real HTTP to it. Nothing here
//! asserts against a hand-built replica of the assembly, because a replica is
//! what let the previous state of this service pass its tests while its `main`
//! did nothing at all.

use mindclade_artifact_proxy::ProxyConfig;
use mindclade_artifact_proxy::bootstrap::{self, BootstrapConfig};
use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_object_store::{ObjectMeta, ObjectPath, ObjectStore, PutCondition, PutResult};
use mindclade_runtime_core::ResourceVersion;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, SystemTime};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::oneshot;

/// Bounded polling budget: 100 attempts at 50ms is five seconds, which is an
/// order of magnitude more than a loopback probe needs and still terminates.
const POLL_ATTEMPTS: u32 = 100;
const POLL_INTERVAL: Duration = Duration::from_millis(50);
const MAX_RESPONSE_BYTES: u64 = 64 * 1024;

/// An object store whose availability is switched at runtime.
///
/// Only the provider is a double; every other part of the assembly under test
/// is the production one. This is how "unready when a dependency is absent" is
/// proven without waiting for a real object store to fail.
#[derive(Debug)]
struct GatedStore {
    available: AtomicBool,
}

impl GatedStore {
    fn new(available: bool) -> Self {
        Self {
            available: AtomicBool::new(available),
        }
    }
    fn set_available(&self, value: bool) {
        self.available.store(value, Ordering::Release);
    }
    fn gate(&self) -> FaultResult<()> {
        if self.available.load(Ordering::Acquire) {
            Ok(())
        } else {
            Err(Fault::new(Code::Unavailable, "object store is unavailable"))
        }
    }
}

impl ObjectStore for GatedStore {
    fn head(&self, _path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        self.gate().map(|()| None)
    }
    fn get(&self, _path: &ObjectPath, _maximum_bytes: ByteSize) -> FaultResult<Vec<u8>> {
        self.gate().map(|()| Vec::new())
    }
    fn get_range(&self, _path: &ObjectPath, _range: ByteRange) -> FaultResult<Vec<u8>> {
        self.gate().map(|()| Vec::new())
    }
    fn put(
        &self,
        path: &ObjectPath,
        bytes: &[u8],
        _condition: PutCondition,
    ) -> FaultResult<PutResult> {
        self.gate()?;
        Ok(PutResult {
            meta: ObjectMeta {
                path: path.clone(),
                size: ByteSize::new(u64::try_from(bytes.len()).unwrap_or(u64::MAX)),
                digest: Digest::from_bytes([0; 32]),
                version: ResourceVersion::new(1, Digest::from_bytes([0; 32])),
                modified: SystemTime::UNIX_EPOCH,
            },
            created: true,
        })
    }
    fn delete(&self, _path: &ObjectPath, _expected: Option<ResourceVersion>) -> FaultResult<bool> {
        self.gate().map(|()| false)
    }
    fn list(&self, _prefix: Option<&ObjectPath>, _limit: usize) -> FaultResult<Vec<ObjectMeta>> {
        self.gate().map(|()| Vec::new())
    }
}

fn temporary_root(label: &str) -> PathBuf {
    let root = std::env::temp_dir().join(format!(
        "mindclade-artifact-proxy-{label}-{}-{:?}",
        std::process::id(),
        std::time::Instant::now(),
    ));
    std::fs::create_dir_all(&root).expect("temporary object-store root");
    root
}

fn config(root: PathBuf) -> BootstrapConfig {
    BootstrapConfig {
        operations_address: "127.0.0.1:0".parse().expect("loopback address"),
        object_store_root: root,
        proxy: ProxyConfig {
            maximum_read_bytes: 1 << 20,
            maximum_write_bytes: 1 << 20,
            maximum_range_bytes: 1 << 16,
            maximum_cache_bytes: 1 << 20,
            maximum_cache_entries: 16,
            maximum_concurrent_transfers: 8,
        },
    }
}

/// One bounded HTTP/1.1 GET. `Connection: close` lets the response end at EOF,
/// so no chunked/keep-alive parsing is needed to read a status and a body.
async fn get(address: SocketAddr, path: &str) -> (u16, String) {
    let mut stream = TcpStream::connect(address).await.expect("connect");
    let request =
        format!("GET {path} HTTP/1.1\r\nHost: artifact-proxy-probe\r\nConnection: close\r\n\r\n");
    stream.write_all(request.as_bytes()).await.expect("request");
    let mut raw = Vec::new();
    stream
        .take(MAX_RESPONSE_BYTES)
        .read_to_end(&mut raw)
        .await
        .expect("response");
    let text = String::from_utf8_lossy(&raw).into_owned();
    let status = text
        .split_whitespace()
        .nth(1)
        .and_then(|code| code.parse::<u16>().ok())
        .unwrap_or_else(|| panic!("unparsable status line in {text:?}"));
    (status, text)
}

async fn poll_until(address: SocketAddr, path: &str, expected: u16) -> String {
    for _ in 0..POLL_ATTEMPTS {
        let (status, body) = get(address, path).await;
        if status == expected {
            return body;
        }
        tokio::time::sleep(POLL_INTERVAL).await;
    }
    panic!("{path} never answered {expected} within the polling budget");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn readiness_becomes_true_and_the_process_stops_on_termination() {
    let root = temporary_root("ready");
    let (address_tx, address_rx) = oneshot::channel();
    let (terminate_tx, terminate_rx) = oneshot::channel();
    let process = tokio::spawn(bootstrap::run_until(
        config(root.clone()),
        async move {
            let _ = terminate_rx.await;
        },
        move |address| {
            let _ = address_tx.send(address);
        },
    ));
    let address = address_rx.await.expect("bound operations address");

    // Readiness genuinely transitions. It cannot have been true before the
    // lifecycle start pass: the registry was empty, and an empty registry is
    // not ready.
    let ready = poll_until(address, "/readyz", 200).await;
    assert!(ready.contains("phase=running"), "{ready}");
    assert!(
        ready.contains("component=artifact-proxy-object-store status=healthy"),
        "{ready}"
    );

    let (live_status, live_body) = get(address, "/healthz").await;
    assert_eq!(live_status, 200, "{live_body}");

    let (metrics_status, metrics_body) = get(address, "/metrics").await;
    assert_eq!(metrics_status, 200);
    assert!(
        metrics_body.contains("artifact_proxy_accounting_healthy 1"),
        "{metrics_body}"
    );

    let _ = terminate_tx.send(());
    let outcome = process.await.expect("process task");
    assert!(outcome.is_ok(), "{outcome:?}");
    let _ = std::fs::remove_dir_all(root);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn readiness_drops_when_the_object_store_stops_answering() {
    let root = temporary_root("degrade");
    let store = Arc::new(GatedStore::new(true));
    let (address_tx, address_rx) = oneshot::channel();
    let (terminate_tx, terminate_rx) = oneshot::channel();
    let process = tokio::spawn(bootstrap::run_with_store(
        config(root.clone()),
        store.clone(),
        async move {
            let _ = terminate_rx.await;
        },
        move |address| {
            let _ = address_tx.send(address);
        },
    ));
    let address = address_rx.await.expect("bound operations address");
    poll_until(address, "/readyz", 200).await;

    // The dependency goes away. Nothing else about the process changes.
    store.set_available(false);
    let unready = poll_until(address, "/readyz", 503).await;
    assert!(
        unready.contains("component=artifact-proxy-object-store status=unhealthy"),
        "{unready}"
    );
    assert!(unready.contains("is unreachable"), "{unready}");

    // Liveness follows readiness down here, and deliberately so: an `Unhealthy`
    // report fails `HealthRegistry::is_live`, which is the semantics
    // `servicekit::server::live` documents. This is recorded as an assertion so
    // that changing it is a decision rather than a surprise.
    let (live_status, _) = get(address, "/healthz").await;
    assert_eq!(live_status, 503);

    // And it recovers: the probe is re-run per request, so nothing latches.
    store.set_available(true);
    poll_until(address, "/readyz", 200).await;

    let _ = terminate_tx.send(());
    assert!(process.await.expect("process task").is_ok());
    let _ = std::fs::remove_dir_all(root);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn an_unreachable_store_fails_the_process_closed_before_it_binds() {
    let root = temporary_root("closed");
    let store = Arc::new(GatedStore::new(false));
    let mut bound = None;
    let outcome = bootstrap::run_with_store(
        config(root.clone()),
        store,
        std::future::pending(),
        |address| bound = Some(address),
    )
    .await;
    let error = outcome.expect_err("an unreachable store must fail the process");
    assert_eq!(error.code(), Code::Unavailable);
    assert!(
        bound.is_none(),
        "the operational listener must not bind before the store answers"
    );
    let _ = std::fs::remove_dir_all(root);
}

#[test]
fn configuration_is_required_rather_than_defaulted() {
    // No artifact-proxy variables are set in this process, so every required
    // input is missing. A composition root that defaulted them would come up
    // enforcing invented byte limits.
    let error = BootstrapConfig::from_env().expect_err("empty environment must fail closed");
    assert_eq!(error.code(), Code::FailedPrecondition);
}
