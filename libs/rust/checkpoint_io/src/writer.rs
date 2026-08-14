//! Checkpoint writer and transactional session.

use crate::{
    CheckpointManifest, CheckpointShard
};
use mindclade_artifact_cas::ArtifactCas;
use mindclade_runtime_core::Clock;
use mindclade_content_digest::{
    hash_bytes, Digest
};
use mindclade_faults::{
    Code, Fault, FaultResult
};
use mindclade_identifiers::ResourceId;
use mindclade_object_store::{
    ObjectPath, ObjectStore, PutCondition
};
use std::collections::BTreeMap;
use std::sync::Arc;

#[derive(Clone)]
pub struct CheckpointWriter {
    cas: ArtifactCas, store: Arc<dyn ObjectStore>, clock: Arc<dyn Clock>
}

impl CheckpointWriter {
    #[must_use] pub fn new(cas: ArtifactCas, store: Arc<dyn ObjectStore>, clock: Arc<dyn Clock>) -> Self {
        Self {
            cas, store, clock
        }
    }
    pub fn begin(&self, run_id: ResourceId, step: u64, world_size: u32, parallel_plan: Digest) -> FaultResult<CheckpointSession> {
        if run_id.kind() != "run" || world_size == 0 {
            return Err(Fault::invalid_argument("checkpoint run ID or world size is invalid"));
        }
        let checkpoint_id = ResourceId::generate("checkpoint", self.clock.as_ref()).map_err(|error| Fault::internal("failed to generate checkpoint ID")
        .with_source(error))?;
        Ok(CheckpointSession {
            writer: self.clone(), checkpoint_id, run_id, step, world_size, parallel_plan, shards: Vec::new(), components: BTreeMap::new(),
            committed: false
        })
    }
}

pub struct CheckpointSession {
    writer: CheckpointWriter,
    checkpoint_id: ResourceId,
    run_id: ResourceId,
    step: u64,
    world_size: u32,
    parallel_plan: Digest,
    shards: Vec<CheckpointShard>,
    components: BTreeMap<String, Digest>,
    committed: bool,
}

impl CheckpointSession {
    #[must_use] pub fn checkpoint_id(&self) -> &ResourceId {
        &self.checkpoint_id
    }
    pub fn write_shard(&mut self, name: impl Into<String>, rank: u32, bytes: &[u8]) -> FaultResult<Digest> {
        if self.committed {
            return Err(Fault::new(Code::FailedPrecondition, "checkpoint session is already committed"));
        }
        let name = name.into();
        if rank >= self.world_size || name.is_empty() || name.len() > 512 || name.starts_with('/') || name.ends_with('/') || name
        .contains("//") || name.split('/').any(|segment| matches!(segment, "." | "..")) {
            return Err(Fault::invalid_argument("checkpoint shard fields are invalid"));
        }
        if self.shards.iter().any(|shard| shard.name == name) {
            return Err(Fault::new(Code::AlreadyExists, "checkpoint shard already exists"));
        }
        let size = u64::try_from(bytes.len())
        .map_err(|_| Fault::new(Code::OutOfRange, "checkpoint shard size exceeds u64"))?;
        let digest = self.writer.cas.put_blob(bytes)?;
        self.shards.push(CheckpointShard {
            name, digest, size, rank
        });
        Ok(digest)
    }
    pub fn register_component(&mut self, name: impl Into<String>, digest: Digest) -> FaultResult<()> {
        let name = name.into();
        if name.is_empty() || name.len() > 256 {
            return Err(Fault::invalid_argument("checkpoint component name is invalid"));
        }
        if self.components.insert(name, digest).is_some() {
            return Err(Fault::new(Code::AlreadyExists, "checkpoint component already exists"));
        }
        Ok(())
    }
    pub fn commit(mut self) -> FaultResult<CheckpointManifest> {
        if self.committed {
            return Err(Fault::new(Code::FailedPrecondition, "checkpoint session is already committed"));
        }
        let manifest = CheckpointManifest {
            checkpoint_id: self.checkpoint_id.clone(), run_id: self.run_id.clone(), step: self.step, world_size: self
            .world_size, parallel_plan: self.parallel_plan, shards: self.shards.clone(), components: self.components
            .clone()
        };
        let encoded = manifest.encode()?;
        let digest = hash_bytes(&encoded);
        let base = format!("checkpoints/{}", self.checkpoint_id);
        let manifest_path = ObjectPath::new(format!("{base}/manifest.mckp")).map_err(|error| Fault::internal(error
        .to_string()))?;
        self.writer.store.put(&manifest_path, &encoded, PutCondition::CreateOnly)?;
        let marker_path = ObjectPath::new(format!("{base}/COMMITTED")).map_err(|error| Fault::internal(error.to_string()))?;
        self.writer.store.put(&marker_path, digest.to_string().as_bytes(), PutCondition::CreateOnly)?;
        self.committed = true;
        Ok(manifest)
    }
}
