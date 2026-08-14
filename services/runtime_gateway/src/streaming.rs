//! Bounded response multiplexing is implemented once in `serving/runtime`.
pub use mindclade_serving_runtime::streaming::{ResponseChunk, StreamReceiver, StreamSender, bounded_stream};
