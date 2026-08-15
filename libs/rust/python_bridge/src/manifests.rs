// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::FaultResult;
use mindclade_manifests::ArtifactManifest;

pub fn decode_artifact_manifest(bytes: &[u8]) -> FaultResult<ArtifactManifest> {
    ArtifactManifest::decode(bytes)
}
pub fn encode_artifact_manifest(manifest: &ArtifactManifest) -> FaultResult<Vec<u8>> {
    manifest.encode()
}
