// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::ArtifactRef;
use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};
use std::collections::BTreeMap;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CheckpointComponentRef {
    pub name: String,
    pub artifact: ArtifactRef,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DistributedCheckpointManifest {
    pub schema_version: u32,
    pub step: u64,
    pub world_size: u32,
    pub topology_digest: Digest,
    pub components: BTreeMap<String, ArtifactRef>,
}

impl DistributedCheckpointManifest {
    pub fn validate(&self) -> FaultResult<()> {
        if self.schema_version == 0 || self.world_size == 0 || self.components.is_empty() {
            return Err(Fault::invalid_argument(
                "distributed checkpoint manifest is incomplete",
            ));
        }
        for (name, a) in &self.components {
            if name.is_empty() || name.len() > 256 {
                return Err(Fault::invalid_argument(
                    "checkpoint component name is invalid",
                ));
            }
            a.validate()?;
        }
        Ok(())
    }
}
