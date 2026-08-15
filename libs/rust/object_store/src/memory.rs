// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic in-memory provider.

use crate::{ObjectMeta, ObjectPath, ObjectStore, PutCondition, PutResult};
use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::ResourceVersion;
use std::collections::BTreeMap;
use std::sync::{Arc, RwLock};
use std::time::SystemTime;

#[derive(Clone, Debug)]
struct Entry {
    bytes: Vec<u8>,
    meta: ObjectMeta,
}

#[derive(Clone, Debug, Default)]
pub struct MemoryStore {
    entries: Arc<RwLock<BTreeMap<ObjectPath, Entry>>>,
}

impl MemoryStore {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

impl ObjectStore for MemoryStore {
    fn head(&self, path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        let guard = self
            .entries
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        Ok(guard.get(path).map(|entry| entry.meta.clone()))
    }
    fn get(&self, path: &ObjectPath, maximum_bytes: ByteSize) -> FaultResult<Vec<u8>> {
        let guard = self
            .entries
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let entry = guard
            .get(path)
            .ok_or_else(|| Fault::new(Code::NotFound, "object not found"))?;
        if entry.meta.size.get() > maximum_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "object exceeds read limit",
            ));
        }
        entry.meta.digest.verify(&entry.bytes)?;
        Ok(entry.bytes.clone())
    }
    fn get_range(&self, path: &ObjectPath, range: ByteRange) -> FaultResult<Vec<u8>> {
        let guard = self
            .entries
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let entry = guard
            .get(path)
            .ok_or_else(|| Fault::new(Code::NotFound, "object not found"))?;
        let start = usize::try_from(range.start())
            .map_err(|_| Fault::new(Code::OutOfRange, "range start exceeds platform limits"))?;
        let end = usize::try_from(range.end())
            .map_err(|_| Fault::new(Code::OutOfRange, "range end exceeds platform limits"))?;
        let Some(slice) = entry.bytes.get(start..end) else {
            return Err(Fault::new(
                Code::OutOfRange,
                "object range exceeds object size",
            ));
        };
        Ok(slice.to_vec())
    }
    fn put(
        &self,
        path: &ObjectPath,
        bytes: &[u8],
        condition: PutCondition,
    ) -> FaultResult<PutResult> {
        let mut guard = self
            .entries
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let current = guard.get(path).map(|entry| entry.meta.version);
        match (condition, current) {
            (PutCondition::Any, _) | (PutCondition::CreateOnly, None) => {}
            (PutCondition::CreateOnly, Some(_)) => {
                return Err(Fault::new(Code::AlreadyExists, "object already exists"));
            }
            (PutCondition::Match(expected), Some(actual)) if expected == actual => {}
            (PutCondition::Match(_), _) => {
                return Err(Fault::new(
                    Code::Conflict,
                    "object version precondition failed",
                ));
            }
        }
        let digest = hash_bytes(bytes);
        let created = current.is_none();
        let version = match current {
            Some(value) => value.next(digest)?,
            None => ResourceVersion::new(1, digest),
        };
        let size = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "object size exceeds u64"))?;
        let meta = ObjectMeta {
            path: path.clone(),
            size: ByteSize::new(size),
            digest,
            version,
            modified: SystemTime::now(),
        };
        guard.insert(
            path.clone(),
            Entry {
                bytes: bytes.to_vec(),
                meta: meta.clone(),
            },
        );
        Ok(PutResult { meta, created })
    }
    fn delete(&self, path: &ObjectPath, expected: Option<ResourceVersion>) -> FaultResult<bool> {
        let mut guard = self
            .entries
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let Some(entry) = guard.get(path) else {
            return Ok(false);
        };
        if expected.is_some_and(|version| version != entry.meta.version) {
            return Err(Fault::new(
                Code::Conflict,
                "object version precondition failed",
            ));
        }
        guard.remove(path);
        Ok(true)
    }
    fn list(&self, prefix: Option<&ObjectPath>, limit: usize) -> FaultResult<Vec<ObjectMeta>> {
        if limit == 0 || limit > 100_000 {
            return Err(Fault::invalid_argument("object list limit is invalid"));
        }
        let prefix = prefix.map(ObjectPath::as_str);
        let guard = self
            .entries
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        Ok(guard
            .values()
            .filter(|entry| prefix.is_none_or(|value| entry.meta.path.as_str().starts_with(value)))
            .take(limit)
            .map(|entry| entry.meta.clone())
            .collect())
    }
}
