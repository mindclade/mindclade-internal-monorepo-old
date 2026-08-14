//! RAII buffer returned to a bounded pool on drop.

use crate::BufferPool;
use mindclade_faults::{Fault, FaultResult};

#[derive(Debug)]
pub struct PooledBuffer<'a> {
    pool: &'a BufferPool,
    buffer: Option<Vec<u8>>,
}

impl<'a> PooledBuffer<'a> {
    pub fn acquire(pool: &'a BufferPool, minimum_capacity: usize) -> FaultResult<Self> {
        Ok(Self {
            pool,
            buffer: Some(pool.take(minimum_capacity)?),
        })
    }
    #[must_use]
    pub fn as_slice(&self) -> &[u8] {
        self.buffer.as_deref().unwrap_or(&[])
    }
    pub fn as_mut(&mut self) -> FaultResult<&mut Vec<u8>> {
        self.buffer
            .as_mut()
            .ok_or_else(|| Fault::internal("pooled buffer was already consumed"))
    }
    pub fn take(mut self) -> FaultResult<Vec<u8>> {
        self.buffer
            .take()
            .ok_or_else(|| Fault::internal("pooled buffer was already consumed"))
    }
}

impl Drop for PooledBuffer<'_> {
    fn drop(&mut self) {
        if let Some(buffer) = self.buffer.take() {
            let _ = self.pool.put(buffer);
        }
    }
}
