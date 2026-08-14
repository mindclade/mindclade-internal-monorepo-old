//! Committed checkpoint reader and verifier.

use crate::CheckpointManifest;
use mindclade_artifact_cas::ArtifactCas;
use mindclade_bytes_io::ByteSize;
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{
    Code, Fault, FaultResult
};
use mindclade_object_store::{
    ObjectPath, ObjectStore
};
use std::sync::Arc;

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct VerificationReport {
    pub verified_shards: usize,
    pub verified_bytes: u64,
    pub failures: Vec<String>
}

impl VerificationReport {
    #[must_use] pub fn is_valid(&self) -> bool {
        self.failures.is_empty()
    }
}

#[derive(Clone)]
pub struct CheckpointReader {
    cas: ArtifactCas, store: Arc<dyn ObjectStore>
}

impl CheckpointReader {
    #[must_use] pub fn new(cas: ArtifactCas, store: Arc<dyn ObjectStore>) -> Self {
        Self {
            cas, store
        }
    }
    pub fn load(&self, checkpoint_id: &str) -> FaultResult<CheckpointManifest> {
        let base = format!("checkpoints/{checkpoint_id}");
        let manifest_path = ObjectPath::new(format!("{base}/manifest.mckp")).map_err(|error| Fault::invalid_argument(error.to_string()))?;
        let marker_path = ObjectPath::new(format!("{base}/COMMITTED")).map_err(|error| Fault::invalid_argument(error.to_string()))?;
        let bytes = self.store.get(&manifest_path, ByteSize::new(64 * 1024 * 1024))?;
        let marker = self.store.get(&marker_path, ByteSize::new(256)).map_err(|error| if error.code() == Code::NotFound {
            Fault::new(Code::FailedPrecondition, "checkpoint is not committed")
        } else {
            error
        })?;
        let marker = std::str::from_utf8(&marker).map_err(|error| Fault::data_loss("checkpoint commit marker is not UTF-8").with_source(error))?;
        if marker.trim() != hash_bytes(&bytes).to_string() {
            return Err(Fault::data_loss("checkpoint commit marker does not match manifest"));
        }
        CheckpointManifest::decode(&bytes)
    }
    pub fn verify(&self, checkpoint_id: &str) -> FaultResult<VerificationReport> {
        let manifest = self.load(checkpoint_id)?;
        let mut report = VerificationReport::default();
        for shard in manifest.shards {
            match self.cas.get_blob(shard.digest) {
                Ok(bytes) => {
                    let size = match u64::try_from(bytes.len()) {
                        Ok(size) => size,
                        Err(_) => {
                            report.failures.push(format!("{}: size exceeds u64", shard.name));
                            continue;
                        }
                    };
                    if size != shard.size {
                        report.failures.push(format!("{}: size mismatch", shard.name));
                        continue;
                    }
                    report.verified_shards = report.verified_shards.checked_add(1)
                    .ok_or_else(|| Fault::new(Code::OutOfRange, "verified shard count overflow"))?;
                    report.verified_bytes = report.verified_bytes.checked_add(shard.size)
                    .ok_or_else(|| Fault::new(Code::OutOfRange, "verified checkpoint byte count overflow"))?;
                }
                Err(error) => report.failures.push(format!("{}: {}", shard.name, error.code())),
            }
        }
        Ok(report)
    }
}
