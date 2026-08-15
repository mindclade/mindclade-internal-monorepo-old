// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Canonical object paths.

use core::fmt;
use core::str::FromStr;
use std::path::{Component, Path};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObjectPathError(&'static str);
impl fmt::Display for ObjectPathError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.0)
    }
}
impl std::error::Error for ObjectPathError {}

/// Relative slash-separated path used by object stores.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ObjectPath(String);
impl ObjectPath {
    pub const MAX_LEN: usize = 1024;
    pub fn new(value: impl Into<String>) -> Result<Self, ObjectPathError> {
        let value = value.into();
        if value.is_empty()
            || value.len() > Self::MAX_LEN
            || value.contains('\\')
            || value.contains('\0')
        {
            return Err(ObjectPathError(
                "object path is empty, too long, or contains unsupported characters",
            ));
        }
        let path = Path::new(&value);
        if path.is_absolute() {
            return Err(ObjectPathError("object path must be relative"));
        }
        for component in path.components() {
            match component {
                Component::Normal(_) => {}
                _ => {
                    return Err(ObjectPathError(
                        "object path contains traversal or platform prefixes",
                    ));
                }
            }
        }
        if value.starts_with('/') || value.ends_with('/') || value.contains("//") {
            return Err(ObjectPathError("object path separators are invalid"));
        }
        Ok(Self(value))
    }
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
    pub fn child(&self, segment: &str) -> Result<Self, ObjectPathError> {
        if segment.is_empty() || segment.contains('/') || segment == "." || segment == ".." {
            return Err(ObjectPathError("object path child segment is invalid"));
        }
        Self::new(format!("{}/{segment}", self.0))
    }
}
impl fmt::Display for ObjectPath {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}
impl FromStr for ObjectPath {
    type Err = ObjectPathError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        Self::new(value)
    }
}
