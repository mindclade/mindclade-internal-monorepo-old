//! Canonical immutable artifact and manifest primitives.
#![forbid(unsafe_code)]
mod artifact;
pub mod checkpoint;
pub mod dataset;
mod reference;
pub mod runtime;
pub mod tensor;
pub mod validation;
pub use artifact::{
    ArtifactManifest, BlobRef, MANIFEST_SCHEMA, MAX_BLOBS, MAX_ENCODED_BYTES, MAX_METADATA
};
pub use checkpoint::{
    CheckpointComponentRef, DistributedCheckpointManifest
};
pub use dataset::{
    DatasetManifest, DatasetShard
};
pub use reference::{
    ArtifactLocation, ArtifactRef
};
pub use runtime::RuntimeManifest;
pub use tensor::TensorManifest;
