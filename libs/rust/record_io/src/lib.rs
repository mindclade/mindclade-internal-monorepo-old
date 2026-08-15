// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded deterministic records, indexes, codecs and recovery-safe framing.
#![forbid(unsafe_code)]
mod codec;
pub mod compression;
mod frame;
pub mod index;
pub mod reader;
pub mod writer;
pub use codec::{Decoder, Encoder};
pub use frame::{FRAME_HEADER_BYTES, FRAME_MAGIC, Record, RecordReader, RecordWriter};
pub use index::{RecordIndex, RecordIndexEntry};
