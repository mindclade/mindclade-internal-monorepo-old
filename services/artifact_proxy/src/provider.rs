// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Provider-backed artifact persistence leaf.
//!
//! Authorization is evaluated before backend access. Provider mechanics remain
//! below the Mindclade ticket/grant contract and never infer tenancy from an
//! object-store path.

use crate::ValidatedGrant;
use bytes::Bytes;
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_object_store::adapters::arrow::ArrowProvider;

#[derive(Clone, Debug)]
pub struct ProviderArtifacts {
    provider: ArrowProvider,
}

impl ProviderArtifacts {
    #[must_use]
    pub fn new(provider: ArrowProvider) -> Self {
        Self { provider }
    }

    pub async fn read_digest(
        &self,
        grant: &ValidatedGrant,
        digest: Digest,
        now_unix_millis: u64,
    ) -> FaultResult<Bytes> {
        grant.authorize_read(digest, now_unix_millis)?;
        let bytes = self
            .provider
            .get(&content_path(digest), Some(digest))
            .await?;
        let size = byte_len(bytes.len())?;
        grant.require_read(digest, size, now_unix_millis)?;
        Ok(bytes)
    }

    /// A range read that is verified before it is returned.
    ///
    /// The prior implementation called `get_range` directly. `get_range` takes
    /// no digest and cannot take one — a partial read has no digest to check
    /// against — so every byte it returned was unverified, from a public method
    /// on a service whose stated invariant is that unverified bytes are never
    /// accepted. It is fetched whole and verified, then sliced, which is the
    /// same tradeoff `TransferEngine::read_range` already makes. Range reads
    /// are therefore bounded by `maximum_read_bytes`, not by the range length;
    /// serving a range of an object too large to verify whole needs a per-chunk
    /// digest in the manifest, which the artifact contract does not carry yet.
    pub async fn read_digest_range(
        &self,
        grant: &ValidatedGrant,
        digest: Digest,
        start: u64,
        length: u64,
        now_unix_millis: u64,
    ) -> FaultResult<Bytes> {
        grant.require_range(digest, length, now_unix_millis)?;
        let bytes = self.read_digest(grant, digest, now_unix_millis).await?;
        let object_size = byte_len(bytes.len())?;
        let end = start
            .checked_add(length)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "artifact range overflow"))?;
        if end > object_size {
            return Err(Fault::new(
                Code::OutOfRange,
                "artifact range exceeds object bounds",
            ));
        }
        let start = usize::try_from(start)
            .map_err(|_| Fault::new(Code::OutOfRange, "range offset exceeds platform usize"))?;
        let end = usize::try_from(end)
            .map_err(|_| Fault::new(Code::OutOfRange, "range end exceeds platform usize"))?;
        Ok(bytes.slice(start..end))
    }

    pub async fn publish_content(
        &self,
        grant: &ValidatedGrant,
        namespace: &str,
        bytes: Bytes,
        now_unix_millis: u64,
    ) -> FaultResult<Digest> {
        if bytes.is_empty() {
            return Err(Fault::new(
                Code::InvalidArgument,
                "artifact content is empty",
            ));
        }
        let size = byte_len(bytes.len())?;
        grant.require_write(namespace, size, now_unix_millis)?;
        let digest = mindclade_content_digest::hash_bytes(&bytes);
        match self.provider.put_create(&content_path(digest), bytes).await {
            Ok(_) => Ok(digest),
            Err(error) if error.code() == Code::AlreadyExists => {
                self.provider
                    .get(&content_path(digest), Some(digest))
                    .await?;
                Ok(digest)
            }
            Err(error) => Err(error),
        }
    }
}

fn content_path(digest: Digest) -> String {
    let text = digest.to_string();
    let hex = text.strip_prefix("sha256:").unwrap_or(&text);
    format!("sha256/{}/{hex}", &hex[..2])
}

fn byte_len(value: usize) -> FaultResult<u64> {
    u64::try_from(value)
        .map_err(|_| Fault::new(Code::OutOfRange, "provider artifact size exceeds u64"))
}
