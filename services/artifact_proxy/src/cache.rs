// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded verified local byte cache with deterministic least-recently-used eviction.

use mindclade_content_digest::{hash_bytes, Digest};
use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CacheEntry {
    pub digest: Digest,
    pub bytes: Arc<[u8]>,
    pub sequence: u64,
}

#[derive(Debug)]
struct State {
    entries: BTreeMap<Digest, CacheEntry>,
    bytes: u64,
    sequence: u64,
}

#[derive(Debug)]
pub struct LocalCache {
    maximum_bytes: u64,
    maximum_entries: usize,
    state: Mutex<State>,
}

impl LocalCache {
    pub fn new(maximum_bytes: u64, maximum_entries: usize) -> FaultResult<Self> {
        if maximum_entries == 0 || maximum_entries > 1_000_000 {
            return Err(Fault::invalid_argument("artifact cache entry limit is invalid"));
        }
        Ok(Self {
            maximum_bytes,
            maximum_entries,
            state: Mutex::new(State { entries: BTreeMap::new(), bytes: 0, sequence: 0 }),
        })
    }
    pub fn get(&self, digest: Digest) -> FaultResult<Option<Arc<[u8]>>> {
        let mut state = self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        let sequence = next_sequence(&mut state)?;
        let Some(entry) = state.entries.get_mut(&digest) else { return Ok(None); };
        entry.sequence = sequence;
        Ok(Some(entry.bytes.clone()))
    }
    pub fn insert(&self, digest: Digest, bytes: Vec<u8>) -> FaultResult<()> {
        if hash_bytes(&bytes) != digest {
            return Err(Fault::data_loss("artifact cache insert digest mismatch"));
        }
        let size = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "artifact cache entry size exceeds u64"))?;
        if self.maximum_bytes != 0 && size > self.maximum_bytes {
            return Err(Fault::new(Code::ResourceExhausted, "artifact exceeds cache capacity"));
        }
        let mut state = self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        if let Some(previous) = state.entries.remove(&digest) {
            let previous_size = u64::try_from(previous.bytes.len())
                .map_err(|_| Fault::new(Code::OutOfRange, "cached entry size exceeds u64"))?;
            state.bytes = state.bytes.checked_sub(previous_size)
                .ok_or_else(|| Fault::data_loss("artifact cache byte accounting underflow"))?;
        }
        let sequence = next_sequence(&mut state)?;
        state.bytes = state.bytes.checked_add(size)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "artifact cache byte accounting overflow"))?;
        state.entries.insert(digest, CacheEntry { digest, bytes: Arc::from(bytes), sequence });
        while state.entries.len() > self.maximum_entries
            || (self.maximum_bytes != 0 && state.bytes > self.maximum_bytes)
        {
            let victim = state.entries.iter()
                .min_by_key(|(digest, entry)| (entry.sequence, **digest))
                .map(|(digest, _)| *digest);
            let Some(victim) = victim else {
                return Err(Fault::data_loss("artifact cache accounting requires eviction but cache is empty"));
            };
            let removed = state.entries.remove(&victim)
                .ok_or_else(|| Fault::data_loss("artifact cache eviction target disappeared"))?;
            let removed_size = u64::try_from(removed.bytes.len())
                .map_err(|_| Fault::new(Code::OutOfRange, "cached entry size exceeds u64"))?;
            state.bytes = state.bytes.checked_sub(removed_size)
                .ok_or_else(|| Fault::data_loss("artifact cache byte accounting underflow"))?;
        }
        Ok(())
    }
    #[must_use]
    pub fn bytes(&self) -> u64 {
        self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner()).bytes
    }
    #[must_use]
    pub fn entries(&self) -> usize {
        self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner()).entries.len()
    }
}

fn next_sequence(state: &mut State) -> FaultResult<u64> {
    if state.sequence == u64::MAX {
        let mut order: Vec<(Digest, u64)> = state.entries.iter()
            .map(|(digest, entry)| (*digest, entry.sequence))
            .collect();
        order.sort_by_key(|(digest, sequence)| (*sequence, *digest));
        for (index, (digest, _)) in order.into_iter().enumerate() {
            let sequence = u64::try_from(index)
                .map_err(|_| Fault::new(Code::OutOfRange, "artifact cache sequence compaction exceeds u64"))?
                .checked_add(1)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "artifact cache sequence compaction overflow"))?;
            state.entries.get_mut(&digest)
                .ok_or_else(|| Fault::data_loss("artifact cache sequence compaction lost an entry"))?
                .sequence = sequence;
        }
        state.sequence = u64::try_from(state.entries.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "artifact cache entry count exceeds u64"))?;
    }
    state.sequence = state.sequence.checked_add(1)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "artifact cache sequence exhausted"))?;
    Ok(state.sequence)
}
