use mindclade_manifests::ArtifactManifest;
use mindclade_faults::FaultResult;

pub fn decode_artifact_manifest(bytes: &[u8]) -> FaultResult<ArtifactManifest> {
    ArtifactManifest::decode(bytes)
}
pub fn encode_artifact_manifest(manifest: &ArtifactManifest) -> FaultResult<Vec<u8>> {
    manifest.encode()
}
