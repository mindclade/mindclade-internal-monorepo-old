// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug)]
pub struct BufferPool {
    inner: Arc<Mutex<State>>,
    max_cached_bytes: usize,
}
#[derive(Debug, Default)]
struct State {
    buffers: Vec<Vec<u8>>,
    cached_bytes: usize,
}

impl BufferPool {
    #[must_use]
    pub fn new(max_cached_bytes: usize) -> Self {
        Self {
            inner: Arc::new(Mutex::new(State::default())),
            max_cached_bytes,
        }
    }
    pub fn take(&self, minimum_capacity: usize) -> FaultResult<Vec<u8>> {
        let absolute_limit = self
            .max_cached_bytes
            .max(1)
            .checked_mul(16)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "buffer-pool request policy overflow"))?;
        if minimum_capacity > absolute_limit {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "requested buffer is outside pool policy",
            ));
        }
        let mut state = self
            .inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if let Some(index) = state
            .buffers
            .iter()
            .position(|buffer| buffer.capacity() >= minimum_capacity)
        {
            let mut buffer = state.buffers.swap_remove(index);
            state.cached_bytes = state
                .cached_bytes
                .checked_sub(buffer.capacity())
                .ok_or_else(|| Fault::data_loss("buffer-pool cached-byte accounting underflow"))?;
            buffer.clear();
            return Ok(buffer);
        }
        Ok(Vec::with_capacity(minimum_capacity))
    }
    #[must_use]
    pub fn put(&self, mut buffer: Vec<u8>) -> bool {
        buffer.clear();
        let capacity = buffer.capacity();
        let mut state = self
            .inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let Some(next) = state.cached_bytes.checked_add(capacity) else {
            return false;
        };
        if next > self.max_cached_bytes {
            return false;
        }
        state.cached_bytes = next;
        state.buffers.push(buffer);
        true
    }
    #[must_use]
    pub fn cached_bytes(&self) -> usize {
        self.inner
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .cached_bytes
    }
}
