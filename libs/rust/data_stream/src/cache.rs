use crate::Shard;
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use std::collections::{BTreeMap, VecDeque};
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug)]
pub struct ShardCache {
    inner: Arc<Mutex<State>>,
    maximum_bytes: u64,
}

#[derive(Debug, Default)]
struct State {
    entries: BTreeMap<Digest, Arc<Vec<u8>>>,
    order: VecDeque<Digest>,
    bytes: u64,
}

impl ShardCache {
    #[must_use]
    pub fn new(maximum_bytes: u64) -> Self {
        Self { inner: Arc::new(Mutex::new(State::default())), maximum_bytes }
    }
    pub fn get(&self, digest: Digest) -> Option<Arc<Vec<u8>>> {
        self.inner
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .entries
            .get(&digest)
            .cloned()
    }
    pub fn insert_verified(&self, shard: &Shard, bytes: Vec<u8>) -> FaultResult<Arc<Vec<u8>>> {
        shard.digest.verify(&bytes)?;
        let byte_len = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "cached shard size exceeds u64"))?;
        if byte_len != shard.size {
            return Err(Fault::data_loss("cached shard size mismatch"));
        }
        if shard.size > self.maximum_bytes {
            return Err(Fault::new(Code::ResourceExhausted, "shard exceeds cache capacity"));
        }
        let mut state = self.inner.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        if let Some(existing) = state.entries.get(&shard.digest) {
            return Ok(existing.clone());
        }
        while state
            .bytes
            .checked_add(shard.size)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "shard-cache byte accounting overflow"))?
            > self.maximum_bytes
        {
            let Some(old_digest) = state.order.pop_front() else {
                return Err(Fault::data_loss("shard-cache accounting has no eviction candidate"));
            };
            if let Some(old) = state.entries.remove(&old_digest) {
                let old_len = u64::try_from(old.len())
                    .map_err(|_| Fault::new(Code::OutOfRange, "cached shard size exceeds u64"))?;
                state.bytes = state
                    .bytes
                    .checked_sub(old_len)
                    .ok_or_else(|| Fault::data_loss("shard-cache byte accounting underflow"))?;
            }
        }
        let value = Arc::new(bytes);
        state.entries.insert(shard.digest, value.clone());
        state.order.push_back(shard.digest);
        state.bytes = state
            .bytes
            .checked_add(shard.size)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "shard-cache byte accounting overflow"))?;
        Ok(value)
    }
    #[must_use]
    pub fn used_bytes(&self) -> u64 {
        self.inner.lock().unwrap_or_else(|poisoned| poisoned.into_inner()).bytes
    }
}
