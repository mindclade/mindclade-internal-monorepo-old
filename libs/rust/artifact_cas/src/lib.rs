// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Content-addressed artifact store.
#![forbid(unsafe_code)]
pub mod blob;
pub mod gc;
pub mod index;
pub mod manifest;
pub mod retention;
pub mod store;
use mindclade_bytes_io::ByteSize;
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_manifests::ArtifactManifest;
use mindclade_object_store::{ObjectPath, ObjectStore, PutCondition};
use mindclade_runtime_core::Clock;
use std::sync::Arc;
use std::time::{Duration, UNIX_EPOCH};

#[derive(Clone, Debug)]
pub struct CasConfig {
    pub maximum_blob_bytes: ByteSize,
    pub maximum_manifest_bytes: ByteSize,
    pub garbage_collection_grace: Duration,
}

impl Default for CasConfig {
    fn default() -> Self {
        Self {
            maximum_blob_bytes: ByteSize::new(1_u64 << 40),
            maximum_manifest_bytes: ByteSize::new(64 * 1024 * 1024),
            garbage_collection_grace: Duration::from_hours(24),
        }
    }
}

#[derive(Clone)]
pub struct ArtifactCas {
    store: Arc<dyn ObjectStore>,
    config: CasConfig,
    clock: Arc<dyn Clock>,
}

impl ArtifactCas {
    pub fn new(
        store: Arc<dyn ObjectStore>,
        clock: Arc<dyn Clock>,
        config: CasConfig,
    ) -> FaultResult<Self> {
        if config.maximum_blob_bytes.get() == 0 || config.maximum_manifest_bytes.get() == 0 {
            return Err(Fault::invalid_argument("CAS size limits must be non-zero"));
        }
        Ok(Self {
            store,
            config,
            clock,
        })
    }
    fn manifest_path(manifest: &ArtifactManifest) -> FaultResult<ObjectPath> {
        ObjectPath::new(format!("cas/manifests/{}.mcam", manifest.artifact_id)).map_err(|error| {
            Fault::internal("failed to construct CAS manifest path").with_source(error)
        })
    }
    pub fn put_blob(&self, bytes: &[u8]) -> FaultResult<Digest> {
        let size = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "CAS blob length exceeds u64"))?;
        if size > self.config.maximum_blob_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "CAS blob exceeds configured limit",
            ));
        }
        let digest = hash_bytes(bytes);
        let path = blob::blob_path(digest)?;
        match self.store.put(&path, bytes, PutCondition::CreateOnly) {
            Ok(_) => Ok(digest),
            Err(error) if error.code() == Code::AlreadyExists => {
                let existing = self.store.get(&path, self.config.maximum_blob_bytes)?;
                digest.verify(&existing)?;
                Ok(digest)
            }
            Err(error) => Err(error),
        }
    }
    pub fn get_blob(&self, digest: Digest) -> FaultResult<Vec<u8>> {
        let bytes = self
            .store
            .get(&blob::blob_path(digest)?, self.config.maximum_blob_bytes)?;
        digest.verify(&bytes)?;
        Ok(bytes)
    }
    pub fn contains_blob(&self, digest: Digest) -> FaultResult<bool> {
        Ok(self.store.head(&blob::blob_path(digest)?)?.is_some())
    }
    pub fn publish_manifest(&self, manifest: &ArtifactManifest) -> FaultResult<Digest> {
        manifest.validate()?;
        for blob in &manifest.blobs {
            let meta = self
                .store
                .head(&blob::blob_path(blob.digest)?)?
                .ok_or_else(|| {
                    Fault::new(
                        Code::FailedPrecondition,
                        "artifact manifest references a missing CAS blob",
                    )
                    .with_context("blob", blob.digest.to_string())
                })?;
            if meta.digest != blob.digest || meta.size.get() != blob.size {
                return Err(Fault::data_loss(
                    "artifact blob metadata does not match manifest",
                ));
            }
        }
        let encoded = manifest.encode()?;
        let encoded_size = u64::try_from(encoded.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "artifact manifest length exceeds u64"))?;
        if encoded_size > self.config.maximum_manifest_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "artifact manifest exceeds configured limit",
            ));
        }
        let digest = hash_bytes(&encoded);
        let path = Self::manifest_path(manifest)?;
        match self.store.put(&path, &encoded, PutCondition::CreateOnly) {
            Ok(_) => Ok(digest),
            Err(error) if error.code() == Code::AlreadyExists => {
                let existing = self.store.get(&path, self.config.maximum_manifest_bytes)?;
                if hash_bytes(&existing) != digest {
                    return Err(Fault::new(
                        Code::Conflict,
                        "artifact ID already has a different manifest",
                    ));
                }
                Ok(digest)
            }
            Err(error) => Err(error),
        }
    }
    pub fn load_manifest(&self, artifact_id: &str) -> FaultResult<ArtifactManifest> {
        let artifact_id = artifact_id.parse::<ResourceId>().map_err(|error| {
            Fault::invalid_argument("artifact ID is invalid").with_source(error)
        })?;
        if artifact_id.kind() != "artifact" {
            return Err(Fault::invalid_argument("artifact ID kind is invalid"));
        }
        let path = ObjectPath::new(format!("cas/manifests/{artifact_id}.mcam"))
            .map_err(|error| Fault::invalid_argument(error.to_string()))?;
        ArtifactManifest::decode(&self.store.get(&path, self.config.maximum_manifest_bytes)?)
    }
    /// Execute a control-plane-produced, version-bound garbage-collection plan.
    ///
    /// Artifact reachability, retention, pins, leases, audit holds, and release
    /// evidence are intentionally absent here; they are Go control-plane policy.
    pub fn sweep_garbage_collection(
        &self,
        plan: &gc::GarbageCollectionPlan,
    ) -> FaultResult<gc::SweepReport> {
        gc::sweep(self.store.as_ref(), plan)
    }
    pub fn current_unix_millis(&self) -> FaultResult<u64> {
        let duration = self
            .clock
            .system_now()
            .duration_since(UNIX_EPOCH)
            .map_err(|error| {
                Fault::new(Code::FailedPrecondition, "CAS clock is before Unix epoch")
                    .with_source(error)
            })?;
        u64::try_from(duration.as_millis())
            .map_err(|_| Fault::new(Code::OutOfRange, "CAS wall-clock milliseconds exceed u64"))
    }
}
