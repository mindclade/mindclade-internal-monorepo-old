//! Pure-Rust SHA-256 digests and streaming verification.
#![forbid(unsafe_code)]
pub mod algorithm;
mod digest;
pub mod reader;
mod sha256;
mod stream;
pub mod writer;
pub use digest::{
    Algorithm, Digest, ParseDigestError
};
pub use sha256::{
    hash_bytes, Sha256
};
pub use stream::{
    hash_reader, DigestingWriter, VerifyingReader
};
