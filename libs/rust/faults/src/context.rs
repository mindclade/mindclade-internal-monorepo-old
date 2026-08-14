//! Structured context with explicit sensitive-value handling.

use std::collections::BTreeMap;
use std::fmt;

/// A typed value attached to a fault.
#[derive(Clone, Debug, Eq, PartialEq)]
#[non_exhaustive]
pub enum ContextValue {
    String(String),
    Signed(i64),
    Unsigned(u64),
    Boolean(bool),
    /// A value intentionally hidden from logs and displays.
    Sensitive,
}

impl fmt::Display for ContextValue {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::String(value) => formatter.write_str(value),
            Self::Signed(value) => write!(formatter, "{value}"),
            Self::Unsigned(value) => write!(formatter, "{value}"),
            Self::Boolean(value) => write!(formatter, "{value}"),
            Self::Sensitive => formatter.write_str("[REDACTED]"),
        }
    }
}

impl From<String> for ContextValue {
    fn from(value: String) -> Self {
        Self::String(value)
    }
}

impl From<&str> for ContextValue {
    fn from(value: &str) -> Self {
        Self::String(value.to_owned())
    }
}

impl From<i64> for ContextValue {
    fn from(value: i64) -> Self {
        Self::Signed(value)
    }
}

impl From<u64> for ContextValue {
    fn from(value: u64) -> Self {
        Self::Unsigned(value)
    }
}

impl From<bool> for ContextValue {
    fn from(value: bool) -> Self {
        Self::Boolean(value)
    }
}

/// Deterministically ordered fault context.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct Context {
    values: BTreeMap<String, ContextValue>,
}

impl Context {
    /// Creates an empty context.
    #[must_use]
    pub const fn new() -> Self {
        Self { values: BTreeMap::new() }
    }
    /// Inserts a non-secret context value.
    pub fn insert(&mut self, key: impl Into<String>, value: impl Into<ContextValue>) {
        self.values.insert(key.into(), value.into());
    }
    /// Marks a key as present but sensitive without retaining its plaintext.
    pub fn insert_sensitive(&mut self, key: impl Into<String>) {
        self.values.insert(key.into(), ContextValue::Sensitive);
    }
    /// Returns a value by key.
    #[must_use]
    pub fn get(&self, key: &str) -> Option<&ContextValue> {
        self.values.get(key)
    }
    /// Iterates in deterministic key order.
    pub fn iter(&self) -> impl Iterator<Item = (&str, &ContextValue)> {
        self.values.iter().map(|(key, value)| (key.as_str(), value))
    }
    /// Whether no values are present.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.values.is_empty()
    }
}
