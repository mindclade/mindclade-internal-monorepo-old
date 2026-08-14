//! Bounded deterministic records, indexes, codecs and recovery-safe framing.
#![forbid(unsafe_code)]
mod codec;
pub mod compression;
mod frame;
pub mod index;
pub mod reader;
pub mod writer;
pub use codec::{
    Decoder, Encoder
};
pub use frame::{
    Record, RecordReader, RecordWriter, FRAME_HEADER_BYTES, FRAME_MAGIC
};
pub use index::{
    RecordIndex, RecordIndexEntry
};
