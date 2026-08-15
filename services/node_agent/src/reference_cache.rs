// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Immutable reference-database snapshot activation cache.
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::RwLock;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReferenceSnapshot {
    pub digest: Digest,
    pub root: PathBuf,
    pub bytes: u64,
    pub activated_unix_millis: u64,
}

impl ReferenceSnapshot {
    pub fn validate(&self) -> FaultResult<()> {
        if self.bytes == 0 || !self.root.is_absolute() {
            return Err(Fault::invalid_argument(
                "reference snapshot cache entry is invalid",
            ));
        }
        Ok(())
    }
}

#[derive(Debug)]
pub struct ReferenceCache {
    maximum_bytes: u64,
    entries: RwLock<BTreeMap<Digest, ReferenceSnapshot>>,
}

impl ReferenceCache {
    pub fn new(maximum_bytes: u64) -> FaultResult<Self> {
        if maximum_bytes == 0 {
            return Err(Fault::invalid_argument(
                "reference cache limit must be positive",
            ));
        }
        Ok(Self {
            maximum_bytes,
            entries: RwLock::new(BTreeMap::new()),
        })
    }
    pub fn activate(&self, snapshot: ReferenceSnapshot) -> FaultResult<()> {
        snapshot.validate()?;
        let mut entries = self.entries.write().unwrap_or_else(|p| p.into_inner());
        let current = entries.values().try_fold(0_u64, |total, entry| {
            total.checked_add(entry.bytes).ok_or_else(|| {
                Fault::new(
                    Code::ResourceExhausted,
                    "reference cache accounting overflow",
                )
            })
        })?;
        let existing = entries.get(&snapshot.digest).map_or(0, |entry| entry.bytes);
        let projected = current
            .checked_sub(existing)
            .and_then(|value| value.checked_add(snapshot.bytes))
            .ok_or_else(|| {
                Fault::new(
                    Code::ResourceExhausted,
                    "reference cache accounting overflow",
                )
            })?;
        if projected > self.maximum_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "reference cache capacity exceeded",
            ));
        }
        entries.insert(snapshot.digest, snapshot);
        Ok(())
    }
    pub fn resolve(&self, digest: Digest) -> FaultResult<PathBuf> {
        self.entries
            .read()
            .unwrap_or_else(|p| p.into_inner())
            .get(&digest)
            .map(|v| v.root.clone())
            .ok_or_else(|| Fault::new(Code::NotFound, "reference snapshot is not active on node"))
    }
    pub fn contains_path(&self, digest: Digest, path: &Path) -> bool {
        self.entries
            .read()
            .unwrap_or_else(|p| p.into_inner())
            .get(&digest)
            .is_some_and(|entry| path.starts_with(&entry.root))
    }
}
