//! Immutable artifact manifests.
#![forbid(unsafe_code)]

use mindclade_content_digest::{
    hash_bytes, Digest
};
use mindclade_faults::{
    Code, Fault, FaultResult
};
use mindclade_identifiers::{
    Name, ResourceId
};
use mindclade_record_io::{
    Decoder, Encoder
};
use std::collections::{
    BTreeMap, BTreeSet
};

pub const MANIFEST_SCHEMA: u16 = 1;
pub const MAX_BLOBS: usize = 1_000_000;
pub const MAX_METADATA: usize = 256;
pub const MAX_ENCODED_BYTES: usize = 64 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BlobRef {
    pub path: Name,
    pub digest: Digest,
    pub size: u64,
    pub media_type: String,
}

impl BlobRef {
    pub fn new(path: Name, digest: Digest, size: u64, media_type: impl Into<String>) -> FaultResult<Self> {
        let media_type = media_type.into();
        if media_type.is_empty() || media_type.len() > 256 || !media_type.is_ascii() {
            return Err(Fault::invalid_argument("artifact blob media type is invalid"));
        }
        Ok(Self {
            path, digest, size, media_type
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ArtifactManifest {
    pub artifact_id: ResourceId,
    pub kind: Name,
    pub logical_size: u64,
    pub blobs: Vec<BlobRef>,
    pub metadata: BTreeMap<String, String>,
}

impl ArtifactManifest {
    pub fn new(artifact_id: ResourceId, kind: Name, blobs: Vec<BlobRef>) -> FaultResult<Self> {
        let logical_size = blobs.iter().try_fold(0_u64, |total, blob| total.checked_add(blob.size).ok_or_else(|| Fault::new(Code::OutOfRange,
        "artifact logical size overflow")))?;
        let manifest = Self {
            artifact_id, kind, logical_size, blobs, metadata: BTreeMap::new()
        };
        manifest.validate()?;
        Ok(manifest)
    }
    pub fn insert_metadata(&mut self, key: impl Into<String>, value: impl Into<String>) -> FaultResult<()> {
        let key = key.into();
        let value = value.into();
        validate_metadata_entry(&key, &value)?;
        if self.metadata.len() >= MAX_METADATA && !self.metadata.contains_key(&key) {
            return Err(Fault::new(Code::ResourceExhausted, "artifact metadata limit exceeded"));
        }
        self.metadata.insert(key, value);
        Ok(())
    }
    pub fn validate(&self) -> FaultResult<()> {
        if self.blobs.is_empty() || self.blobs.len() > MAX_BLOBS {
            return Err(Fault::invalid_argument("artifact blob count is invalid"));
        }
        if self.metadata.len() > MAX_METADATA {
            return Err(Fault::invalid_argument("artifact metadata count is invalid"));
        }
        for (key, value) in &self.metadata {
            validate_metadata_entry(key, value)?;
        }
        let mut paths = BTreeSet::new();
        let mut total = 0_u64;
        for blob in &self.blobs {
            if !paths.insert(blob.path.as_str()) {
                return Err(Fault::new(Code::AlreadyExists, "artifact contains duplicate blob paths"));
            }
            total = total.checked_add(blob.size).ok_or_else(|| Fault::new(Code::OutOfRange, "artifact logical size overflow"))?;
        }
        if total != self.logical_size {
            return Err(Fault::data_loss("artifact logical size does not match blob inventory"));
        }
        Ok(())
    }
    pub fn encode(&self) -> FaultResult<Vec<u8>> {
        self.validate()?;
        let estimated_blob_bytes = self.blobs.len().checked_mul(96)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "artifact manifest capacity calculation overflow"))?;
        let estimated_capacity = 256_usize.checked_add(estimated_blob_bytes)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "artifact manifest capacity calculation overflow"))?
        .min(MAX_ENCODED_BYTES);
        let mut encoder = Encoder::with_capacity(estimated_capacity);
        encoder.u16(MANIFEST_SCHEMA);
        encoder.string(&self.artifact_id.to_string())?;
        encoder.string(self.kind.as_str())?;
        encoder.u64(self.logical_size);
        encoder.u32(u32::try_from(self.blobs.len()).map_err(|_| Fault::new(Code::OutOfRange, "artifact blob count exceeds u32"))?);
        for blob in &self.blobs {
            encoder.string(blob.path.as_str())?;
            encoder.bytes(blob.digest.as_bytes())?;
            encoder.u64(blob.size);
            encoder.string(&blob.media_type)?;
        }
        encoder.u32(u32::try_from(self.metadata.len()).map_err(|_| Fault::new(Code::OutOfRange, "artifact metadata count exceeds u32"))?);
        for (key, value) in &self.metadata {
            encoder.string(key)?;
            encoder.string(value)?;
        }
        let bytes = encoder.into_bytes();
        if bytes.len() > MAX_ENCODED_BYTES {
            return Err(Fault::new(Code::ResourceExhausted, "artifact manifest exceeds encoded size limit"));
        }
        Ok(bytes)
    }
    pub fn decode(bytes: &[u8]) -> FaultResult<Self> {
        let mut decoder = Decoder::new(bytes, MAX_ENCODED_BYTES)?;
        if decoder.u16()? != MANIFEST_SCHEMA {
            return Err(Fault::new(Code::FailedPrecondition, "artifact manifest schema is unsupported"));
        }
        let artifact_id = decoder.string()?.parse::<ResourceId>().map_err(|error| Fault::data_loss("artifact ID is invalid")
        .with_source(error))?;
        let kind = Name::new(decoder.string()?).map_err(|error| Fault::data_loss("artifact kind is invalid").with_source(error))?;
        let logical_size = decoder.u64()?;
        let blob_count = decoder.item_count()?;
        if blob_count == 0 || blob_count > MAX_BLOBS {
            return Err(Fault::data_loss("artifact blob count is invalid"));
        }
        let mut blobs = Vec::with_capacity(blob_count);
        for _ in 0..blob_count {
            let path = Name::new(decoder.string()?).map_err(|error| Fault::data_loss("artifact blob path is invalid")
            .with_source(error))?;
            let digest_bytes = decoder.bytes()?;
            let digest_array = <[u8; 32]>::try_from(digest_bytes).map_err(|_| Fault::data_loss("artifact blob digest length is invalid"))?;
            let digest = Digest::from_bytes(digest_array);
            let size = decoder.u64()?;
            let media_type = decoder.string()?.to_owned();
            blobs.push(BlobRef::new(path, digest, size, media_type)?);
        }
        let metadata_count = decoder.item_count()?;
        if metadata_count > MAX_METADATA {
            return Err(Fault::data_loss("artifact metadata count is invalid"));
        }
        let mut metadata = BTreeMap::new();
        for _ in 0..metadata_count {
            let key = decoder.string()?.to_owned();
            let value = decoder.string()?.to_owned();
            if metadata.insert(key, value).is_some() {
                return Err(Fault::data_loss("artifact metadata contains duplicate keys"));
            }
        }
        decoder.finish()?;
        let manifest = Self {
            artifact_id, kind, logical_size, blobs, metadata
        };
        manifest.validate()?;
        Ok(manifest)
    }
    pub fn digest(&self) -> FaultResult<Digest> {
        Ok(hash_bytes(&self.encode()?))
    }
}

fn validate_metadata_entry(key: &str, value: &str) -> FaultResult<()> {
    if key.is_empty()
    || key.len() > 128
    || value.len() > 4096
    || !key.bytes().all(|byte| {
        byte.is_ascii_lowercase()
        || byte.is_ascii_digit()
        || matches!(byte, b'.' | b'-' | b'_')
    })
    {
        return Err(Fault::invalid_argument("artifact metadata entry is invalid"));
    }
    Ok(())
}
