// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded readiness probes for the providers this process composes.
//!
//! A readiness answer that never consults the backing store is a readiness
//! answer about nothing: the process would keep reporting ready with its
//! object store unreachable, and the load balancer would keep sending it byte
//! reads it cannot serve. The probe below is the cheapest question that has a
//! real answer — one metadata read — and it is bounded and deadlined so a
//! wedged provider cannot wedge the probe path with it.
//!
//! This is deliberately service-local rather than a shared helper. Under
//! ADR-0010 services are composition roots and do not export reusable
//! mechanism; `node_agent` carries its own copy for the same reason.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_object_store::{ObjectPath, ObjectStore};
use std::sync::Arc;
use std::time::Duration;

/// Probe object read by readiness. It is never written and need not exist:
/// `head` answers `Ok(None)` for an absent object, so an empty store is a
/// healthy store and the probe stays free of side effects.
const PROBE_OBJECT: &str = "cas/blobs/.mindclade-readiness-probe";

#[derive(Clone)]
pub struct ObjectStoreProbe {
    store: Arc<dyn ObjectStore>,
    path: ObjectPath,
}

impl core::fmt::Debug for ObjectStoreProbe {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("ObjectStoreProbe")
            .field("path", &self.path)
            .finish_non_exhaustive()
    }
}

impl ObjectStoreProbe {
    pub fn new(store: Arc<dyn ObjectStore>) -> FaultResult<Self> {
        let path = ObjectPath::new(PROBE_OBJECT)
            .map_err(|error| Fault::internal("readiness probe path is invalid").with_source(error))?;
        Ok(Self { store, path })
    }

    /// One bounded metadata read on the owning thread.
    ///
    /// Used at startup, before the operational listener exists, so a node that
    /// cannot reach its artifact store fails closed instead of coming up and
    /// advertising a store it has never spoken to.
    pub fn check_blocking(&self) -> FaultResult<()> {
        self.store.head(&self.path).map(|_| ())
    }

    /// The same read moved onto the bounded blocking pool and given a deadline.
    ///
    /// `ObjectStore` is synchronous by contract ("cancellation at call
    /// boundary"), so the deadline bounds the *wait* rather than the read: a
    /// single metadata call that overruns it is reported as a probe timeout and
    /// finishes on the blocking pool, which the runtime owns and joins at
    /// shutdown. Nothing is retried here — the caller's next readiness probe is
    /// the retry, and that is what keeps the loop bounded.
    ///
    /// What this does and does not catch: `head` is a provider round-trip, so a
    /// store that has gone unreachable, lost its credentials, or failed over
    /// fails here. A `LocalStore` whose root directory is deleted underneath a
    /// running process answers `Ok(None)` rather than failing — that class of
    /// local fault is caught by `LocalStore::new` at startup instead, and the
    /// distinction is recorded here rather than left for someone to discover
    /// from a readiness probe that stayed green through an unmounted volume.
    pub async fn check(&self, budget: Duration) -> FaultResult<()> {
        let store = Arc::clone(&self.store);
        let path = self.path.clone();
        let read = tokio::task::spawn_blocking(move || store.head(&path));
        match tokio::time::timeout(budget, read).await {
            Ok(Ok(result)) => result.map(|_| ()),
            Ok(Err(error)) => Err(Fault::internal("readiness probe task failed").with_source(error)),
            Err(_) => Err(Fault::new(
                Code::DeadlineExceeded,
                "artifact object-store readiness probe exceeded its budget",
            )
            .with_context("budget_millis", budget.as_millis().to_string())),
        }
    }
}
