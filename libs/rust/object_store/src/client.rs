// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{
    ClientConfig, Namespace, ObjectMeta, ObjectStore, PutCondition, PutResult, StoreMetrics,
    verification,
};
use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::Arc;

#[derive(Clone)]
pub struct Client {
    store: Arc<dyn ObjectStore>,
    namespace: Namespace,
    config: ClientConfig,
    metrics: Arc<StoreMetrics>,
}
impl Client {
    pub fn new(
        store: Arc<dyn ObjectStore>,
        namespace: Namespace,
        config: ClientConfig,
    ) -> FaultResult<Self> {
        Ok(Self {
            store,
            namespace,
            config: config.validate()?,
            metrics: Arc::new(StoreMetrics::default()),
        })
    }
    #[must_use]
    pub fn metrics(&self) -> Arc<StoreMetrics> {
        self.metrics.clone()
    }
    pub fn head(&self, relative: &str) -> FaultResult<Option<ObjectMeta>> {
        self.store.head(&self.namespace.qualify(relative)?)
    }
    pub fn get(&self, relative: &str, expected: Option<Digest>) -> FaultResult<Vec<u8>> {
        let path = self.namespace.qualify(relative)?;
        let bytes = self
            .store
            .get(&path, self.config.maximum_read_bytes)
            .inspect_err(|_| self.metrics.record_failure())?;
        if let Some(digest) = expected {
            verification::verify_bytes(digest, &bytes)?;
        }
        self.metrics.record_read(byte_len(bytes.len())?);
        Ok(bytes)
    }
    pub fn get_range(&self, relative: &str, range: ByteRange) -> FaultResult<Vec<u8>> {
        if range.length() > self.config.maximum_read_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "range exceeds client limit",
            ));
        }
        let bytes = self
            .store
            .get_range(&self.namespace.qualify(relative)?, range)
            .inspect_err(|_| self.metrics.record_failure())?;
        self.metrics.record_read(byte_len(bytes.len())?);
        Ok(bytes)
    }
    pub fn put(
        &self,
        relative: &str,
        bytes: &[u8],
        condition: PutCondition,
    ) -> FaultResult<PutResult> {
        let size = byte_len(bytes.len())?;
        if size > self.config.maximum_write_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "write exceeds client limit",
            ));
        }
        let result = self
            .store
            .put(&self.namespace.qualify(relative)?, bytes, condition)
            .inspect_err(|_| self.metrics.record_failure())?;
        self.metrics.record_write(size);
        Ok(result)
    }
    pub fn list(&self, relative_prefix: Option<&str>) -> FaultResult<Vec<ObjectMeta>> {
        let prefix = match relative_prefix {
            Some(value) => Some(self.namespace.qualify(value)?),
            None => Some(self.namespace.prefix().clone()),
        };
        self.store
            .list(prefix.as_ref(), self.config.maximum_list_items)
    }
}

fn byte_len(value: usize) -> FaultResult<u64> {
    u64::try_from(value)
        .map_err(|_| Fault::new(Code::OutOfRange, "object-store byte count exceeds u64"))
}
