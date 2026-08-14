//! Checked byte arithmetic, bounded copying, and reusable process-local buffers.
#![forbid(unsafe_code)]
mod copy;
mod pool;
pub mod alignment;
pub mod buffer;
pub mod metrics;
pub mod range;
mod spec;
pub mod vectored;
pub use copy::{
    copy_bounded, CopyReport
};
pub use pool::BufferPool;
pub use spec::{
    Alignment, ByteBudget, ByteRange, ByteSize, Reservation
};
