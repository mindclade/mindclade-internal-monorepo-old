// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Async provider adapter backed by Apache Arrow `object_store`.
//!
//! Provider mechanics live here; tenant namespaces, digest verification, byte
//! limits and content-addressed policy remain Mindclade-owned.

use crate::{ClientConfig, Namespace};
use bytes::Bytes;
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use object_store::path::Path as ProviderPath;
use object_store::{ObjectStore as ProviderStore, ObjectStoreExt, PutMode, PutOptions, PutPayload};
use std::sync::Arc;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProviderMeta {
    pub location: String,
    pub size: u64,
    pub etag: Option<String>,
    pub version: Option<String>,
}

#[derive(Clone)]
pub struct ArrowProvider {
    inner: Arc<dyn ProviderStore>,
    namespace: Namespace,
    config: ClientConfig,
}

impl core::fmt::Debug for ArrowProvider {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("ArrowProvider")
            .field("namespace", self.namespace.prefix())
            .field("config", &self.config)
            .finish_non_exhaustive()
    }
}

impl ArrowProvider {
    pub fn new(
        inner: Arc<dyn ProviderStore>,
        namespace: Namespace,
        config: ClientConfig,
    ) -> FaultResult<Self> {
        Ok(Self {
            inner,
            namespace,
            config: config.validate()?,
        })
    }
    pub fn memory(namespace: Namespace, config: ClientConfig) -> FaultResult<Self> {
        Self::new(
            Arc::new(object_store::memory::InMemory::new()),
            namespace,
            config,
        )
    }

    pub fn gcs(bucket: &str, namespace: Namespace, config: ClientConfig) -> FaultResult<Self> {
        if bucket.is_empty() || bucket.len() > 222 {
            return Err(Fault::invalid_argument("GCS bucket name is invalid"));
        }
        let store = object_store::gcp::GoogleCloudStorageBuilder::new()
            .with_bucket_name(bucket)
            .build()
            .map_err(provider_error("failed to configure GCS object store"))?;
        Self::new(Arc::new(store), namespace, config)
    }
    pub fn s3(
        bucket: &str,
        region: &str,
        namespace: Namespace,
        config: ClientConfig,
    ) -> FaultResult<Self> {
        if bucket.is_empty() || bucket.len() > 255 || region.is_empty() || region.len() > 128 {
            return Err(Fault::invalid_argument("S3 bucket or region is invalid"));
        }
        let store = object_store::aws::AmazonS3Builder::new()
            .with_bucket_name(bucket)
            .with_region(region)
            .build()
            .map_err(provider_error("failed to configure S3 object store"))?;
        Self::new(Arc::new(store), namespace, config)
    }
    pub async fn head(&self, relative: &str) -> FaultResult<ProviderMeta> {
        let path = self.path(relative)?;
        let meta = self
            .inner
            .head(&path)
            .await
            .map_err(provider_error("provider object head failed"))?;
        Ok(provider_meta(meta))
    }
    pub async fn get(&self, relative: &str, expected: Option<Digest>) -> FaultResult<Bytes> {
        let path = self.path(relative)?;
        let result = self
            .inner
            .get(&path)
            .await
            .map_err(provider_error("provider object read failed"))?;
        if result.meta.size > self.config.maximum_read_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "provider object exceeds read budget",
            ));
        }
        let bytes = result
            .bytes()
            .await
            .map_err(provider_error("provider object body read failed"))?;
        if let Some(expected) = expected {
            expected.verify(&bytes)?;
        }
        Ok(bytes)
    }
    pub async fn get_range(&self, relative: &str, start: u64, length: u64) -> FaultResult<Bytes> {
        if length == 0 || length > self.config.maximum_read_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "provider range exceeds read budget",
            ));
        }
        let end = start
            .checked_add(length)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "provider range end overflow"))?;
        self.inner
            .get_range(&self.path(relative)?, start..end)
            .await
            .map_err(provider_error("provider range read failed"))
    }
    pub async fn put_create(&self, relative: &str, bytes: Bytes) -> FaultResult<ProviderMeta> {
        self.put(relative, bytes, PutMode::Create).await
    }
    pub async fn put_overwrite(&self, relative: &str, bytes: Bytes) -> FaultResult<ProviderMeta> {
        self.put(relative, bytes, PutMode::Overwrite).await
    }
    async fn put(&self, relative: &str, bytes: Bytes, mode: PutMode) -> FaultResult<ProviderMeta> {
        let length = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "provider write length exceeds u64"))?;
        if length == 0 || length > self.config.maximum_write_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "provider write exceeds write budget",
            ));
        }
        let path = self.path(relative)?;
        let options = PutOptions {
            mode,
            ..Default::default()
        };
        self.inner
            .put_opts(&path, PutPayload::from_bytes(bytes), options)
            .await
            .map_err(provider_error("provider object write failed"))?;
        self.head(relative).await
    }
    fn path(&self, relative: &str) -> FaultResult<ProviderPath> {
        let qualified = self.namespace.qualify(relative)?;
        ProviderPath::parse(qualified.as_str()).map_err(|error| {
            Fault::invalid_argument("provider object path is invalid").with_source(error)
        })
    }
}

fn provider_meta(meta: object_store::ObjectMeta) -> ProviderMeta {
    ProviderMeta {
        location: meta.location.to_string(),
        size: meta.size,
        etag: meta.e_tag,
        version: meta.version,
    }
}

fn provider_error(message: &'static str) -> impl FnOnce(object_store::Error) -> Fault {
    move |error| {
        let code = match &error {
            object_store::Error::NotFound { .. } => Code::NotFound,
            object_store::Error::AlreadyExists { .. } => Code::AlreadyExists,
            object_store::Error::Precondition { .. } => Code::Conflict,
            object_store::Error::PermissionDenied { .. } => Code::PermissionDenied,
            object_store::Error::Unauthenticated { .. } => Code::Unauthenticated,
            object_store::Error::InvalidPath { .. } => Code::InvalidArgument,
            object_store::Error::NotSupported { .. }
            | object_store::Error::NotImplemented { .. } => Code::Unimplemented,
            object_store::Error::NotModified { .. } => Code::FailedPrecondition,
            _ => Code::Unavailable,
        };
        Fault::new(code, message).with_source(error)
    }
}
