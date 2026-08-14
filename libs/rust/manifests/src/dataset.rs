use crate::{
    validation, ArtifactRef
};
use mindclade_content_digest::{
    hash_bytes, Digest
};
use mindclade_faults::{
    Code, Fault, FaultResult
};

#[derive(Clone, Debug, Eq, PartialEq)]pub struct DatasetShard {
    pub artifact: ArtifactRef, pub records: u64
}

#[derive(Clone, Debug, Eq, PartialEq)]pub struct DatasetManifest {
    pub schema_version: u32, pub dataset_digest: Digest, pub shards: Vec<DatasetShard>
}

impl DatasetManifest {
    pub fn validate(&self) -> FaultResult<()> {
        validation::validate_schema_version(self.schema_version)?;
        if self.shards.is_empty() {
            return Err(Fault::invalid_argument("dataset manifest has no shards"));
        }
        let mut total=0u64;
        for s in &self.shards {
            s.artifact.validate()?;
            total=total.checked_add(s.records).ok_or_else(||Fault::new(Code::OutOfRange, "dataset record count overflow"))?;
        }
        let expected=self.computed_digest()?;
        if expected!=self.dataset_digest {
            return Err(Fault::data_loss("dataset manifest digest mismatch"));
        }
        Ok(())
    }
    pub fn computed_digest(&self) -> FaultResult<Digest> {
        let mut b=Vec::new();
        b.extend_from_slice(&self.schema_version.to_be_bytes());
        for s in &self.shards {
            b.extend_from_slice(s.artifact.digest.as_bytes());
            b.extend_from_slice(&s.records.to_be_bytes());
        }
        Ok(hash_bytes(&b))
    }
}
