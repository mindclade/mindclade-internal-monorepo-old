// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Pure-Rust SHA-256 digests and streaming verification.
#![forbid(unsafe_code)]
pub mod algorithm;
mod digest;
pub mod reader;
mod sha256;
mod stream;
pub mod writer;
pub use digest::{Algorithm, Digest, ParseDigestError};
pub use sha256::{Sha256, hash_bytes};
pub use stream::{DigestingWriter, VerifyingReader, hash_reader};
