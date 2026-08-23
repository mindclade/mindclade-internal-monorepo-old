// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded parser diagnostic with byte/line/column location.
use crate::Location;
use mindclade_faults::{Fault, FaultResult};

/// Longest diagnostic code a parser may declare.
pub const MAXIMUM_CODE_BYTES: usize = 128;

/// Longest diagnostic message that may be retained.
///
/// A recovery-mode reporter naturally quotes the construct it rejected, and an
/// offending line may be as long as `Limits::maximum_line_bytes` — a megabyte
/// by default. The message is therefore truncated to this bound rather than
/// rejected; see [`Diagnostic::truncated`].
pub const MAXIMUM_MESSAGE_BYTES: usize = 4096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Diagnostic {
    pub location: Location,
    pub code: &'static str,
    pub message: String,
}

impl Diagnostic {
    pub fn validate(&self) -> FaultResult<()> {
        if self.code.is_empty()
            || self.code.len() > MAXIMUM_CODE_BYTES
            || self.message.len() > MAXIMUM_MESSAGE_BYTES
        {
            return Err(Fault::invalid_argument("parser diagnostic exceeds bounds"));
        }
        Ok(())
    }

    /// Returns this diagnostic with its message clamped to the retention bound.
    ///
    /// The message is data derived from untrusted input; the code is a constant
    /// the parser author chose. Only the former may legitimately be oversized,
    /// so only the former is clamped — an out-of-bounds `code` stays an error
    /// because it is a bug in the parser rather than a property of the input.
    #[must_use]
    pub fn truncated(mut self) -> Self {
        if self.message.len() > MAXIMUM_MESSAGE_BYTES {
            // `String::truncate` panics on a non-char boundary, and the message
            // may quote arbitrary UTF-8 from the input, so walk back to one.
            let mut end = MAXIMUM_MESSAGE_BYTES;
            while end > 0 && !self.message.is_char_boundary(end) {
                end -= 1;
            }
            self.message.truncate(end);
        }
        self
    }
}
