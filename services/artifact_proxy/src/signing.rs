//! Signed-download claim validation for the artifact byte plane.

use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::{DetachedSignature, SignatureVerifier};

const CLAIM_DOMAIN: &[u8] = b"mindclade.artifact.download.v1\0";
const MAX_TENANT_BYTES: usize = 256;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DownloadClaim {
    pub digest: Digest,
    pub tenant_id: String,
    pub expires_unix_millis: u64,
    pub maximum_bytes: u64,
}

impl DownloadClaim {
    pub fn validate(&self, now_unix_millis: u64) -> FaultResult<()> {
        if self.tenant_id.is_empty()
            || self.tenant_id.len() > MAX_TENANT_BYTES
            || self.tenant_id.bytes().any(|byte| byte == 0)
            || self.expires_unix_millis <= now_unix_millis
            || self.maximum_bytes == 0
        {
            return Err(Fault::invalid_argument("signed download claim is invalid or expired"));
        }
        Ok(())
    }
    pub fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        let tenant_length = u32::try_from(self.tenant_id.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "artifact download tenant length exceeds u32"))?;
        let mut bytes = Vec::with_capacity(
            CLAIM_DOMAIN.len() + 32 + 4 + self.tenant_id.len() + 8 + 8,
        );
        bytes.extend_from_slice(CLAIM_DOMAIN);
        bytes.extend_from_slice(self.digest.as_bytes());
        bytes.extend_from_slice(&tenant_length.to_be_bytes());
        bytes.extend_from_slice(self.tenant_id.as_bytes());
        bytes.extend_from_slice(&self.expires_unix_millis.to_be_bytes());
        bytes.extend_from_slice(&self.maximum_bytes.to_be_bytes());
        Ok(bytes)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedDownload {
    pub claim: DownloadClaim,
    pub signature: DetachedSignature,
}

impl SignedDownload {
    pub fn verify<V: SignatureVerifier + ?Sized>(
        &self,
        now_unix_millis: u64,
        verifier: &V,
    ) -> FaultResult<()> {
        self.claim.validate(now_unix_millis)?;
        verifier.verify(&self.claim.canonical_bytes()?, &self.signature)
    }
}
