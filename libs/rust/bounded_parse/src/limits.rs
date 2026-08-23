// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Ceilings every bounded parser runs under.
//!
//! Every ceiling in this struct is **inclusive**: a value exactly equal to the
//! ceiling is accepted and the next one is rejected. That is stated per field
//! and asserted in `tests/adversarial.rs`, because a limit whose inclusivity is
//! only implied by the comparison operator at the call site is a limit that
//! drifts.

use mindclade_bytes_io::ByteSize;
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Limits {
    /// Total untrusted bytes a parse may be handed. Rejected by `Source::new`
    /// when `bytes.len() > maximum_input_bytes`.
    pub maximum_input_bytes: ByteSize,
    /// Bytes in one line, counting a trailing `\r` but not the `\n`. Rejected by
    /// `Cursor::next_line` when `line > maximum_line_bytes`, so CRLF input
    /// cannot smuggle a byte past the ceiling.
    pub maximum_line_bytes: usize,
    /// Records a parse may retain. Parsers refuse to start record
    /// `maximum_records + 1`, so at most `maximum_records` are ever held.
    pub maximum_records: usize,
    /// Tokens — sequence residues, mmCIF lexemes — a parse may retain, counted
    /// across all records rather than per record.
    pub maximum_tokens: usize,
    /// Metadata-shaped entries a parse may retain, including the recovery-mode
    /// diagnostic sink (see [`crate::Recovery`]).
    pub maximum_metadata_entries: usize,
    /// Structural nesting depth a parse may descend to. No format framed by
    /// `bio_formats` today is recursive, so nothing consumes this yet; it exists
    /// so that a nested format cannot be added without an explicit depth budget.
    pub maximum_nesting: usize,
    /// Payload bytes a parse may charge to its `AllocationBudget`.
    ///
    /// Named for what it counts, not for what it sounds like it counts. This
    /// is *not* a memory ceiling: it accounts for the payload a parse retains
    /// — sequence residues, ids, record bodies — and not for the `Vec` slots,
    /// `String`/`Vec` headers, or allocator block overhead that hold them. A
    /// 64 MiB FASTA of five-byte records charges roughly 2 MiB here while
    /// retaining well over 100 MiB. Memory stays hard-bounded by
    /// `maximum_records`; this ceiling is the payload budget, and an operator
    /// tuning it should know which of the two they are setting.
    pub maximum_payload_bytes: ByteSize,
}

impl Default for Limits {
    fn default() -> Self {
        Self {
            maximum_input_bytes: ByteSize::new(64 * 1024 * 1024),
            maximum_line_bytes: 1 << 20,
            maximum_records: 1_000_000,
            maximum_tokens: 10_000_000,
            maximum_metadata_entries: 1024,
            maximum_nesting: 64,
            maximum_payload_bytes: ByteSize::new(256 * 1024 * 1024),
        }
    }
}

impl Limits {
    pub fn validate(self) -> FaultResult<Self> {
        if self.maximum_input_bytes.get() == 0
            || self.maximum_line_bytes == 0
            || self.maximum_records == 0
            || self.maximum_tokens == 0
            || self.maximum_metadata_entries == 0
            || self.maximum_nesting == 0
            || self.maximum_payload_bytes.get() == 0
        {
            return Err(Fault::invalid_argument("parse limits must be positive"));
        }
        let maximum_reasonable_input =
            self.maximum_payload_bytes
                .get()
                .checked_mul(4)
                .ok_or_else(|| {
                    Fault::new(Code::OutOfRange, "parse payload/input ratio overflows u64")
                })?;
        if self.maximum_input_bytes.get() > maximum_reasonable_input {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "input/allocation limits are inconsistent",
            ));
        }
        Ok(self)
    }
}
