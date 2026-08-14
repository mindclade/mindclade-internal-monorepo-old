//! Validated hierarchical names.

use core::fmt;
use core::str::FromStr;

/// Error returned when a name violates the canonical grammar.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NameError(&'static str);

impl fmt::Display for NameError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.0)
    }
}

impl std::error::Error for NameError {}

/// Slash-separated, relative, lowercase resource name.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct Name(String);

impl Name {
    /// Maximum encoded name length.
    pub const MAX_LEN: usize = 512;
    /// Validates and constructs a name.
    pub fn new(value: impl Into<String>) -> Result<Self, NameError> {
        let value = value.into();
        validate(&value)?;
        Ok(Self(value))
    }
    /// Returns the canonical string.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
    /// Adds one validated child segment.
    pub fn child(&self, segment: &str) -> Result<Self, NameError> {
        validate_segment(segment)?;
        Self::new(format!("{}/{segment}", self.0))
    }
}

impl fmt::Display for Name {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl FromStr for Name {
    type Err = NameError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        Self::new(value)
    }
}

fn validate(value: &str) -> Result<(), NameError> {
    if value.is_empty() {
        return Err(NameError("name must not be empty"));
    }
    if value.len() > Name::MAX_LEN {
        return Err(NameError("name exceeds maximum length"));
    }
    if value.starts_with('/') || value.ends_with('/') || value.contains("//") {
        return Err(NameError("name separators are invalid"));
    }
    for segment in value.split('/') {
        validate_segment(segment)?;
    }
    Ok(())
}

fn validate_segment(segment: &str) -> Result<(), NameError> {
    if segment.is_empty() || segment.len() > 128 || matches!(segment, "." | "..") {
        return Err(NameError("name segment is invalid"));
    }
    if !segment.bytes().all(|byte| {
        byte.is_ascii_lowercase()
            || byte.is_ascii_digit()
            || matches!(byte, b'-' | b'_' | b'.')
    }) {
        return Err(NameError("name segment contains unsupported characters"));
    }
    Ok(())
}
