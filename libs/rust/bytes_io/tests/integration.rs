use mindclade_bytes_io::{
    ByteBudget, ByteRange, ByteSize, BufferPool, copy_bounded
};
use std::io::Cursor;

#[test] fn bounded_copy_and_pool_obey_limits() {
    let mut out=Vec::new();
    let report=copy_bounded(Cursor::new(b"abcdef"), &mut out, 6, 2).expect("copy");
    assert_eq!(report.bytes, 6);
    assert_eq!(out, b"abcdef");
    assert!(copy_bounded(Cursor::new(b"abcdef"), Vec::new(), 5, 2).is_err());
    let budget=ByteBudget::new(ByteSize::new(8));
    let r=budget.reserve(ByteSize::new(8)).expect("reserve");
    assert!(budget.reserve(ByteSize::new(1)).is_err());
    drop(r);
    let pool=BufferPool::new(1024);
    let b=pool.take(128).expect("buffer");
    pool.put(b);
    assert!(pool.cached_bytes()>=128);
    assert!(ByteRange::new(1, 2).is_ok());
}
