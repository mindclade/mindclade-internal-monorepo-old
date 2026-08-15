// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Relative path validation.

use core::fmt;
use std::path::{Component, Path, PathBuf};

/// Invalid relative path.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RelativePathError(&'static str);

impl fmt::Display for RelativePathError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.0)
    }
}
impl std::error::Error for RelativePathError {}

/// Canonical, root-confined relative path.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct RelativePath(PathBuf);

impl RelativePath {
    pub fn new(value: impl AsRef<Path>) -> Result<Self, RelativePathError> {
        let value = value.as_ref();
        if value.as_os_str().is_empty() || value.is_absolute() {
            return Err(RelativePathError("path must be non-empty and relative"));
        }
        let mut normalized = PathBuf::new();
        for component in value.components() {
            match component {
                Component::Normal(segment) if !segment.is_empty() => normalized.push(segment),
                _ => return Err(RelativePathError("path contains an unsupported component")),
            }
        }
        if normalized.as_os_str().is_empty() {
            return Err(RelativePathError("path must contain a normal component"));
        }
        Ok(Self(normalized))
    }
    #[must_use]
    pub fn as_path(&self) -> &Path {
        &self.0
    }
}
