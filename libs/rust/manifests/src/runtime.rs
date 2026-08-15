// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::ArtifactRef;
use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RuntimeManifest {
    pub runtime_bundle: ArtifactRef,
    pub engine_bundle: ArtifactRef,
    pub model_bundle: Option<ArtifactRef>,
    pub minimum_runtime_version: String,
    pub configuration_digest: Digest,
}

impl RuntimeManifest {
    pub fn validate(&self) -> FaultResult<()> {
        self.runtime_bundle.validate()?;
        self.engine_bundle.validate()?;
        if let Some(m) = &self.model_bundle {
            m.validate()?;
        }
        if self.minimum_runtime_version.trim().is_empty() {
            return Err(Fault::invalid_argument("minimum runtime version is empty"));
        }
        Ok(())
    }
}
