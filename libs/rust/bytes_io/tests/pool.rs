// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::{BufferPool, buffer::PooledBuffer};

#[test]
fn pooled_buffer_returns_capacity() {
    let pool = BufferPool::new(1024);
    {
        let mut buffer = PooledBuffer::acquire(&pool, 32).unwrap();
        buffer.as_mut().unwrap().extend_from_slice(b"abc");
        assert_eq!(buffer.as_slice(), b"abc");
    }
    assert!(pool.cached_bytes() >= 32);
}

#[test]
fn taking_buffer_transfers_ownership_without_returning_it_to_pool() {
    let pool = BufferPool::new(1024);
    let mut buffer = PooledBuffer::acquire(&pool, 64).unwrap();
    buffer.as_mut().unwrap().extend_from_slice(b"payload");
    let owned = buffer.take().unwrap();
    assert_eq!(owned, b"payload");
    assert_eq!(pool.cached_bytes(), 0);
}
