// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Verified CAS transfer mechanics.

use crate::{LocalCache, ProxyMetrics, RangeRequest, ValidatedGrant};
use mindclade_artifact_cas::{
    ArtifactCas,
    gc::{GarbageCollectionPlan, SweepReport},
};
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::Arc;

#[derive(Clone)]
pub struct TransferEngine {
    cas: ArtifactCas,
    cache: Arc<LocalCache>,
    metrics: ProxyMetrics,
    maximum_range_bytes: u64,
}

impl TransferEngine {
    #[must_use]
    pub fn new(
        cas: ArtifactCas,
        cache: Arc<LocalCache>,
        metrics: ProxyMetrics,
        maximum_range_bytes: u64,
    ) -> Self {
        Self {
            cas,
            cache,
            metrics,
            maximum_range_bytes,
        }
    }

    pub fn read(
        &self,
        grant: &ValidatedGrant,
        digest: Digest,
        now_unix_millis: u64,
    ) -> FaultResult<Vec<u8>> {
        // Authorize before a cache/provider lookup so an unauthorized caller
        // cannot use hit/miss or backend timing as an artifact-existence oracle.
        grant.authorize_read(digest, now_unix_millis)?;
        if let Some(bytes) = self.cache.get(digest)? {
            let size = byte_len(bytes.len())?;
            grant.require_read(digest, size, now_unix_millis)?;
            self.metrics.cache_hit();
            self.metrics.read(size);
            return Ok(bytes.to_vec());
        }

        let bytes = self.cas.get_blob(digest)?;
        let size = byte_len(bytes.len())?;
        grant.require_read(digest, size, now_unix_millis)?;
        self.cache.insert(digest, bytes.clone())?;
        self.metrics.read(size);
        Ok(bytes)
    }

    pub fn read_range(
        &self,
        grant: &ValidatedGrant,
        digest: Digest,
        request: RangeRequest,
        now_unix_millis: u64,
    ) -> FaultResult<Vec<u8>> {
        grant.authorize_read(digest, now_unix_millis)?;
        if !grant.grant.allow_range_reads {
            return Err(Fault::new(
                Code::PermissionDenied,
                "range reads are not allowed by grant",
            ));
        }

        let bytes = self.read(grant, digest, now_unix_millis)?;
        let object_size = byte_len(bytes.len())?;
        let range = request.validate(object_size, self.maximum_range_bytes)?;
        grant.require_range(digest, range.length(), now_unix_millis)?;
        let start = usize::try_from(range.start())
            .map_err(|_| Fault::new(Code::OutOfRange, "range offset exceeds platform usize"))?;
        let end = usize::try_from(range.end())
            .map_err(|_| Fault::new(Code::OutOfRange, "range end exceeds platform usize"))?;
        Ok(bytes[start..end].to_vec())
    }

    pub fn write(
        &self,
        grant: &ValidatedGrant,
        namespace: &str,
        bytes: &[u8],
        now_unix_millis: u64,
    ) -> FaultResult<Digest> {
        let size = byte_len(bytes.len())?;
        grant.require_write(namespace, size, now_unix_millis)?;
        let digest = self.cas.put_blob(bytes)?;
        // Cache coherence is part of the proxy's local verified-data contract;
        // if the configured cache cannot represent a just-written object, fail
        // the request rather than pretending the local tier is healthy.
        self.cache.insert(digest, bytes.to_vec())?;
        self.metrics.write(size);
        Ok(digest)
    }

    /// Execute an immutable, control-plane-authorized GC plan through the same
    /// versioned object-store path used by normal artifact traffic.
    pub fn sweep_garbage_collection(
        &self,
        plan: &GarbageCollectionPlan,
    ) -> FaultResult<SweepReport> {
        self.cas.sweep_garbage_collection(plan)
    }
}

fn byte_len(value: usize) -> FaultResult<u64> {
    u64::try_from(value)
        .map_err(|_| Fault::new(Code::OutOfRange, "artifact byte count exceeds u64"))
}
