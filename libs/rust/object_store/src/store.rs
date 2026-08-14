//! Store contract and conditional mutation semantics.

use crate::ObjectPath;
use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_content_digest::Digest;
use mindclade_faults::FaultResult;
use mindclade_runtime_core::ResourceVersion;
use std::time::SystemTime;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObjectMeta {
    pub path: ObjectPath,
    pub size: ByteSize,
    pub digest: Digest,
    pub version: ResourceVersion,
    pub modified: SystemTime,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PutCondition {
    Any,
    CreateOnly,
    Match(ResourceVersion),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PutResult { pub meta: ObjectMeta, pub created: bool }

/// Synchronous, cancellation-at-call-boundary object-store mechanism.
/// Network adapters should execute provider calls in their owning async runtime.
pub trait ObjectStore: Send + Sync {
    fn head(&self, path: &ObjectPath) -> FaultResult<Option<ObjectMeta>>;
    fn get(&self, path: &ObjectPath, maximum_bytes: ByteSize) -> FaultResult<Vec<u8>>;
    fn get_range(&self, path: &ObjectPath, range: ByteRange) -> FaultResult<Vec<u8>>;
    fn put(&self, path: &ObjectPath, bytes: &[u8], condition: PutCondition) -> FaultResult<PutResult>;
    fn delete(&self, path: &ObjectPath, expected: Option<ResourceVersion>) -> FaultResult<bool>;
    fn list(&self, prefix: Option<&ObjectPath>, limit: usize) -> FaultResult<Vec<ObjectMeta>>;
}
