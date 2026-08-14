//! Receipt returned after immutable artifact-manifest publication.
use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};
use mindclade_identifiers::ResourceId;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ManifestReceipt {
    pub artifact_id: ResourceId,
    pub manifest_digest: Digest,
    pub logical_size: u64,
}
impl ManifestReceipt {
    pub fn validate(&self) -> FaultResult<()> {
        if self.artifact_id.kind() != "artifact" || self.logical_size == 0 {
            return Err(Fault::invalid_argument("artifact manifest receipt is invalid"));
        }
        Ok(())
    }
}
