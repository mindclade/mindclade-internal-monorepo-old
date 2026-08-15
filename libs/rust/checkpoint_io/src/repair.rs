// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{CheckpointManifest, VerificationReport};

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct RepairPlan {
    pub missing_or_corrupt_shards: Vec<String>,
}

impl RepairPlan {
    #[must_use]
    pub fn from_report(_manifest: &CheckpointManifest, report: &VerificationReport) -> Self {
        let missing_or_corrupt_shards = report
            .failures
            .iter()
            .filter_map(|f| f.split(':').next())
            .map(str::to_owned)
            .collect();
        Self {
            missing_or_corrupt_shards,
        }
    }
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.missing_or_corrupt_shards.is_empty()
    }
}
