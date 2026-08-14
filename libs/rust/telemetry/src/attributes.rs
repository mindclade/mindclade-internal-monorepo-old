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
            Self::Redacted => formatter.write_str("[REDACTED]"),
        }
    }
}

impl From<&str> for AttributeValue { fn from(value: &str) -> Self { Self::String(value.to_owned()) } }
impl From<String> for AttributeValue { fn from(value: String) -> Self { Self::String(value) } }
impl From<i64> for AttributeValue { fn from(value: i64) -> Self { Self::Signed(value) } }
impl From<u64> for AttributeValue { fn from(value: u64) -> Self { Self::Unsigned(value) } }
impl From<bool> for AttributeValue { fn from(value: bool) -> Self { Self::Boolean(value) } }

/// Deterministically ordered attributes with fixed cardinality and value bounds.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Attributes(BTreeMap<String, AttributeValue>);

impl Attributes {
    pub const MAX_ATTRIBUTES: usize = 64;
    pub const MAX_KEY_LEN: usize = 64;
    pub const MAX_STRING_LEN: usize = 2_048;
    #[must_use]
    pub const fn new() -> Self { Self(BTreeMap::new()) }
    pub fn insert(&mut self, key: impl Into<String>, value: impl Into<AttributeValue>) -> bool {
        if self.0.len() >= Self::MAX_ATTRIBUTES { return false; }
        let key = key.into();
        let value = value.into();
        if key.is_empty() || key.len() > Self::MAX_KEY_LEN { return false; }
        if matches!(&value, AttributeValue::String(text) if text.len() > Self::MAX_STRING_LEN) {
            return false;
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
}
