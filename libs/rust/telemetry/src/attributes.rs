// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded structured attributes.

use std::collections::BTreeMap;
use std::fmt;

/// Value safe for structured telemetry.
#[derive(Clone, Debug, PartialEq)]
#[non_exhaustive]
pub enum AttributeValue {
    String(String),
    Signed(i64),
    Unsigned(u64),
    Float(f64),
    Boolean(bool),
    Redacted,
}

impl fmt::Display for AttributeValue {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::String(value) => formatter.write_str(value),
            Self::Signed(value) => write!(formatter, "{value}"),
            Self::Unsigned(value) => write!(formatter, "{value}"),
            Self::Float(value) => write!(formatter, "{value}"),
            Self::Boolean(value) => write!(formatter, "{value}"),
            Self::Redacted => formatter.write_str(REDACTED_TEXT),
        }
    }
}

/// Rendering of a redacted value. Byte-identical to `libs/go/faults.RedactedValue`
/// so one log pipeline can match redactions from either tier.
pub const REDACTED_TEXT: &str = "[REDACTED]";

impl From<&str> for AttributeValue {
    fn from(value: &str) -> Self {
        Self::String(value.to_owned())
    }
}
impl From<String> for AttributeValue {
    fn from(value: String) -> Self {
        Self::String(value)
    }
}
impl From<i64> for AttributeValue {
    fn from(value: i64) -> Self {
        Self::Signed(value)
    }
}
impl From<u64> for AttributeValue {
    fn from(value: u64) -> Self {
        Self::Unsigned(value)
    }
}
impl From<f64> for AttributeValue {
    fn from(value: f64) -> Self {
        Self::Float(value)
    }
}
impl From<bool> for AttributeValue {
    fn from(value: bool) -> Self {
        Self::Boolean(value)
    }
}

/// Deterministically ordered attributes with fixed cardinality and value bounds.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Attributes(BTreeMap<String, AttributeValue>);

impl Attributes {
    pub const MAX_ATTRIBUTES: usize = 64;
    pub const MAX_KEY_LEN: usize = 64;
    pub const MAX_STRING_LEN: usize = 2_048;

    /// Keys the record envelope itself occupies.
    ///
    /// Attributes render as sibling members of the envelope (the shape
    /// `log/slog`'s JSON handler produces on the Go side), so an attribute
    /// named `msg` would emit a second `msg` member in one object. Duplicate
    /// JSON keys are explicitly undefined by RFC 8259 — parsers variously take
    /// the first, take the last, or error — which means the collision would
    /// decide at the *consumer* whether the envelope or the attribute survives.
    /// Rejecting the key here keeps the encoder total and the record
    /// unambiguous, and does so at the point where the caller can see it.
    pub const RESERVED_KEYS: [&'static str; 4] = ["time", "level", "msg", "event.id"];

    #[must_use]
    pub const fn new() -> Self {
        Self(BTreeMap::new())
    }

    /// Inserts a bounded attribute. Returns false when the key is reserved or
    /// out of bounds, when the set is full, or when the value is unrenderable.
    pub fn insert(&mut self, key: impl Into<String>, value: impl Into<AttributeValue>) -> bool {
        let key = key.into();
        let value = value.into();
        if key.is_empty() || key.len() > Self::MAX_KEY_LEN {
            return false;
        }
        if Self::RESERVED_KEYS.contains(&key.as_str()) {
            return false;
        }
        // Cardinality is checked after the key is known so that overwriting an
        // attribute already present stays possible at exactly MAX_ATTRIBUTES.
        // The previous order rejected every insert once the map was full,
        // including the correcting overwrite of a key already counted.
        if self.0.len() >= Self::MAX_ATTRIBUTES && !self.0.contains_key(&key) {
            return false;
        }
        match &value {
            AttributeValue::String(text) if text.len() > Self::MAX_STRING_LEN => return false,
            // JSON has no NaN or Infinity literal, and neither does the Go
            // tier: `observability.normalizeAttributeValue` rejects non-finite
            // floats rather than inventing a spelling for them. Refusing at
            // insert keeps every downstream encoder total.
            AttributeValue::Float(number) if !number.is_finite() => return false,
            // Every remaining shape is admissible as written. `Float` and
            // `String` reappear because their arms above are guarded, and a
            // guard cannot prove coverage.
            AttributeValue::String(_)
            | AttributeValue::Signed(_)
            | AttributeValue::Unsigned(_)
            | AttributeValue::Float(_)
            | AttributeValue::Boolean(_)
            | AttributeValue::Redacted => {}
        }
        self.0.insert(key, value);
        true
    }

    pub fn insert_redacted(&mut self, key: impl Into<String>) -> bool {
        self.insert(key, AttributeValue::Redacted)
    }

    pub fn iter(&self) -> impl Iterator<Item = (&str, &AttributeValue)> {
        self.0.iter().map(|(key, value)| (key.as_str(), value))
    }

    #[must_use]
    pub fn len(&self) -> usize {
        self.0.len()
    }

    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}
