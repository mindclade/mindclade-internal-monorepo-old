// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Last-known-good configuration with explicit restart-required reporting.
//!
//! A reload that touches a field nobody declared reloadable is refused and the
//! previous snapshot is retained. The alternative — applying whatever changed —
//! silently half-reconfigures a running process, which is worse than refusing:
//! the operator believes the new value took effect.
//!
//! `std::sync::RwLock` only. This crate creates no async runtime and no
//! background task; a reload is a call the owning process makes when it decides
//! to make one.

use crate::error;
use crate::snapshot::{Origin, Snapshot};
use mindclade_faults::FaultResult;
use std::sync::RwLock;

/// A single mutable slot holding the current good configuration.
#[derive(Debug)]
pub struct AtomicConfig {
    namespace: String,
    current: RwLock<Snapshot>,
}

impl AtomicConfig {
    /// Seeds the slot with the configuration the process started on.
    #[must_use]
    pub fn new(initial: Snapshot) -> Self {
        Self {
            namespace: initial.namespace().to_owned(),
            current: RwLock::new(initial),
        }
    }

    /// Returns the current configuration.
    pub fn snapshot(&self) -> FaultResult<Snapshot> {
        // A poisoned lock means a writer panicked mid-update. Handing back a
        // possibly torn configuration is exactly the fail-open this file exists
        // to prevent, so the read fails instead.
        self.current
            .read()
            .map(|guard| Snapshot::clone(&guard))
            .map_err(|_| error::internal(&self.namespace, "configuration lock is poisoned"))
    }

    /// Applies a reload, or refuses it and keeps the last known good snapshot.
    pub fn apply(&self, next: Snapshot) -> FaultResult<()> {
        if next.namespace() != self.namespace {
            return Err(error::invalid(
                &self.namespace,
                "",
                "reload snapshot belongs to a different configuration namespace",
            ));
        }
        let mut guard = self
            .current
            .write()
            .map_err(|_| error::internal(&self.namespace, "configuration lock is poisoned"))?;
        if guard.equivalent(&next) {
            return Ok(());
        }
        for (key, current) in guard.values() {
            match next.values().get(key) {
                Some(candidate) if candidate == current => {}
                _ => Self::require_reloadable(&self.namespace, &guard, &next, key)?,
            }
        }
        for key in next.values().keys() {
            if !guard.values().contains_key(key) {
                Self::require_reloadable(&self.namespace, &guard, &next, key)?;
            }
        }
        *guard = next;
        Ok(())
    }

    /// A field must be reloadable in *both* snapshots. Trusting only the
    /// incoming one would let a reload declare itself reloadable.
    fn require_reloadable(
        namespace: &str,
        current: &Snapshot,
        next: &Snapshot,
        key: &str,
    ) -> FaultResult<()> {
        // Fail closed on a key the snapshot has no origin for: an unknown
        // provenance is not evidence that a change is safe to apply live.
        let reloadable = |snapshot: &Snapshot| {
            snapshot
                .origins()
                .get(key)
                .is_some_and(Origin::is_reloadable)
        };
        if reloadable(current) && reloadable(next) {
            return Ok(());
        }
        Err(error::restart_required(namespace, key))
    }
}
