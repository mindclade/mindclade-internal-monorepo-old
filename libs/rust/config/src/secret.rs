// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! A configuration value that must never be printed.
//!
//! This generalizes the `Secret` newtype that `services/ai_gateway_proxy` had
//! grown locally. The rule it encodes is that redaction has to be a property of
//! the *type*, not of every call site: a `String` that happens to hold a token
//! is one `{:?}` away from a log line, and there is no review process that
//! catches that reliably.
//!
//! `[REDACTED]` is the same marker `mindclade_telemetry`'s
//! `AttributeValue::Redacted` and `mindclade_faults`' `ContextValue::Sensitive`
//! render, so a redacted value looks identical wherever it surfaces.
//!
//! Deliberately absent: `Deref`, `AsRef<str>`, `Into<String>`, and `Display` of
//! the plaintext. Reaching the value requires naming [`Secret::expose`], which
//! is greppable in review.

use std::fmt;

/// Marker rendered anywhere a secret would otherwise be printed.
pub const REDACTED: &str = "[REDACTED]";

/// A string value excluded from `Debug`, `Display`, logs, and digests.
#[derive(Clone, Default)]
pub struct Secret(String);

impl Secret {
    /// Wraps a value as a secret.
    #[must_use]
    pub fn new(value: impl Into<String>) -> Self {
        Self(value.into())
    }

    /// Returns the plaintext. Every call site is an auditable exposure point.
    #[must_use]
    pub fn expose(&self) -> &str {
        &self.0
    }

    /// Whether the secret holds no bytes.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }

    /// Byte length of the plaintext. Length is not treated as sensitive.
    #[must_use]
    pub fn len(&self) -> usize {
        self.0.len()
    }
}

impl fmt::Debug for Secret {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(REDACTED)
    }
}

impl fmt::Display for Secret {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(REDACTED)
    }
}

impl PartialEq for Secret {
    /// Compares in time independent of *where* two equal-length secrets differ.
    ///
    /// Length is compared first and therefore leaks; that is the same trade the
    /// digest makes, and a token's length is not the part worth protecting.
    fn eq(&self, other: &Self) -> bool {
        let left = self.0.as_bytes();
        let right = other.0.as_bytes();
        if left.len() != right.len() {
            return false;
        }
        let mut difference = 0_u8;
        for (first, second) in left.iter().zip(right.iter()) {
            difference |= first ^ second;
        }
        difference == 0
    }
}

impl Eq for Secret {}

#[cfg(test)]
mod tests {
    use super::Secret;

    #[test]
    fn never_renders_plaintext() {
        let secret = Secret::new("super-secret-token");
        assert_eq!(format!("{secret:?}"), "[REDACTED]");
        assert_eq!(format!("{secret}"), "[REDACTED]");
        assert!(!format!("{secret:?} {secret}").contains("super-secret-token"));
        assert_eq!(secret.expose(), "super-secret-token");
    }

    #[test]
    fn compares_by_value() {
        assert_eq!(Secret::new("a-token"), Secret::new("a-token"));
        assert_ne!(Secret::new("a-token"), Secret::new("b-token"));
        assert_ne!(Secret::new("a-token"), Secret::new("a-token-longer"));
    }
}
