// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Artifact verification helpers.
use mindclade_content_digest::Digest;
use mindclade_faults::FaultResult;
use mindclade_manifests::ArtifactManifest;

pub fn verify_bytes(expected: Digest, bytes: &[u8]) -> FaultResult<()> {
    expected.verify(bytes)
}
pub fn verify_manifest(bytes: &[u8]) -> FaultResult<ArtifactManifest> {
    ArtifactManifest::decode(bytes)
}
