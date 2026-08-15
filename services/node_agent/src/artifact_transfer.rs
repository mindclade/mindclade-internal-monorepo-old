// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Artifact transfer adapter over the canonical content-addressed store.

use mindclade_artifact_cas::ArtifactCas;
use mindclade_content_digest::Digest;
use mindclade_faults::FaultResult;

#[derive(Clone)]
pub struct ArtifactTransfer {
    cas: ArtifactCas,
}

impl ArtifactTransfer {
    #[must_use]
    pub fn new(cas: ArtifactCas) -> Self {
        Self { cas }
    }
    pub fn fetch(&self, digest: Digest) -> FaultResult<Vec<u8>> {
        self.cas.get_blob(digest)
    }
    pub fn store(&self, bytes: &[u8]) -> FaultResult<Digest> {
        self.cas.put_blob(bytes)
    }
}
