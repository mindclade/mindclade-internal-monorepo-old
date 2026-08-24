// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Canonical digest value.

use core::fmt;
use core::str::FromStr;
use mindclade_faults::{Fault, FaultResult};

/// Supported content digest algorithms.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum Algorithm {
    Sha256,
}

impl Algorithm {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Sha256 => "sha256",
        }
    }
}

/// A SHA-256 content digest.
#[derive(Clone, Copy, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct Digest([u8; 32]);

impl Digest {
    pub const ZERO: Self = Self([0; 32]);
    #[must_use]
    pub const fn from_bytes(bytes: [u8; 32]) -> Self {
        Self(bytes)
    }
    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; 32] {
        &self.0
    }
    #[must_use]
    pub const fn algorithm(self) -> Algorithm {
        Algorithm::Sha256
    }
    #[must_use]
    pub fn to_hex(self) -> String {
        const HEX: &[u8; 16] = b"0123456789abcdef";
        let mut output = String::with_capacity(64);
        for byte in self.0 {
            output.push(char::from(HEX[usize::from(byte >> 4)]));
            output.push(char::from(HEX[usize::from(byte & 0x0f)]));
        }
        output
    }
    /// Constant-time equality over the digest bytes.
    #[must_use]
    pub fn constant_time_eq(self, other: Self) -> bool {
        let mut difference = 0_u8;
        for (left, right) in self.0.iter().zip(other.0) {
            difference |= *left ^ right;
        }
        difference == 0
    }
    /// Verifies bytes against this digest.
    pub fn verify(self, content: &[u8]) -> FaultResult<()> {
        if self.constant_time_eq(crate::hash_bytes(content)) {
            Ok(())
        } else {
            Err(Fault::data_loss("content digest mismatch"))
        }
    }
}

impl fmt::Display for Digest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "sha256:{}", self.to_hex())
    }
}

impl fmt::Debug for Digest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_tuple("Digest")
            .field(&self.to_string())
            .finish()
    }
}

/// Digest parsing failure.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ParseDigestError(&'static str);

impl fmt::Display for ParseDigestError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.0)
    }
}

impl std::error::Error for ParseDigestError {}

impl FromStr for Digest {
    type Err = ParseDigestError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        let Some(encoded) = value.strip_prefix("sha256:") else {
            return Err(ParseDigestError("digest must use the sha256 prefix"));
        };
        if encoded.len() != 64 {
            return Err(ParseDigestError(
                "sha256 digest must contain 64 hexadecimal characters",
            ));
        }
        let mut bytes = [0_u8; 32];
        for (index, pair) in encoded.as_bytes().chunks_exact(2).enumerate() {
            let Some(high) = nibble(pair[0]) else {
                return Err(rejected(pair[0]));
            };
            let Some(low) = nibble(pair[1]) else {
                return Err(rejected(pair[1]));
            };
            bytes[index] = (high << 4) | low;
        }
        Ok(Self(bytes))
    }
}

fn nibble(value: u8) -> Option<u8> {
    match value {
        b'0'..=b'9' => Some(value - b'0'),
        b'a'..=b'f' => Some(value - b'a' + 10),
        _ => None,
    }
}

/// Explains a byte this decoder refused.
///
/// Uppercase is valid hexadecimal, so reporting it as "non-hexadecimal" would send a reader
/// looking for the wrong defect. It is refused because it is not the canonical spelling: this
/// decoder used to accept it while `libs/go/identifiers.ParseDigest` and
/// `libs/python/identifiers.Digest.parse` both rejected it, which gave one content address two
/// spellings across a language boundary -- the same value hashing to two different cache keys
/// depending on which plane parsed it. The wording tracks Go's, so a reader who greps the
/// message finds both implementations.
fn rejected(value: u8) -> ParseDigestError {
    if value.is_ascii_hexdigit() && value.is_ascii_uppercase() {
        return ParseDigestError("digest hexadecimal value must be lowercase");
    }
    ParseDigestError("digest contains a non-hexadecimal character")
}
