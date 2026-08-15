// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};

/// Immutable logical identity. Object-store paths are locations, never identity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ArtifactRef {
    pub digest: Digest,
    pub size_bytes: u64,
    pub media_type: String,
    pub logical_kind: String,
    pub schema_version: u32,
}

impl ArtifactRef {
    pub fn validate(&self) -> FaultResult<()> {
        if self.media_type.is_empty() || self.media_type.len() > 256 || !self.media_type.is_ascii()
        {
            return Err(Fault::invalid_argument("artifact media type is invalid"));
        }
        if self.logical_kind.is_empty()
            || self.logical_kind.len() > 128
            || !self.logical_kind.is_ascii()
        {
            return Err(Fault::invalid_argument("artifact logical kind is invalid"));
        }
        if self.schema_version == 0 {
            return Err(Fault::invalid_argument(
                "artifact schema version must be non-zero",
            ));
        }
        Ok(())
    }
}

/// Provider-specific placement metadata separated from immutable identity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ArtifactLocation {
    pub artifact: ArtifactRef,
    pub provider: String,
    pub uri: String,
    pub generation: String,
    pub region: Option<String>,
}

impl ArtifactLocation {
    pub fn validate(&self) -> FaultResult<()> {
        self.artifact.validate()?;
        if self.provider.is_empty() || self.uri.is_empty() {
            return Err(Fault::invalid_argument("artifact location is incomplete"));
        }
        Ok(())
    }
}
