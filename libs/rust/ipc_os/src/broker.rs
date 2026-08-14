//! Bounded registry for local bulk-data segments.
//!
//! The broker centralizes the OS transport policy used by runtime-host and
//! node-agent consumers.  It reserves registry capacity before creating the
//! underlying segment, keeps each segment alive for the lifetime of its
//! descriptor lease, and verifies content on every read.

use crate::{file::FileSegment, BulkSegment};
#[cfg(target_os = "linux")]
use crate::MemfdSegment;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::BufferDescriptor;
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

/// Operating-system backend used for newly published bulk segments.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum BulkBackend {
    /// Anonymous file-descriptor-backed segments on Linux.
    Memfd,
    /// Portable bounded file segments rooted under an absolute directory.
    LocalFile(PathBuf),
}

impl BulkBackend {
    pub fn local_file(directory: impl Into<PathBuf>) -> FaultResult<Self> {
        let directory = directory.into();
        validate_directory(&directory)?;
        Ok(Self::LocalFile(directory))
    }
}

impl Default for BulkBackend {
    fn default() -> Self {
        Self::Memfd
    }
}

/// Owns live OS segments and enforces bounded segment count/size.
pub struct BulkBufferBroker {
    backend: BulkBackend,
    maximum_segments: usize,
    maximum_bytes_per_segment: u64,
    segments: Mutex<BTreeMap<String, Arc<dyn BulkSegment>>>,
}

impl core::fmt::Debug for BulkBufferBroker {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("BulkBufferBroker")
            .field("backend", &self.backend)
            .field("maximum_segments", &self.maximum_segments)
            .field("maximum_bytes_per_segment", &self.maximum_bytes_per_segment)
            .field("active", &self.active())
            .finish()
    }
}

impl BulkBufferBroker {
    pub fn new(maximum_segments: usize, maximum_bytes_per_segment: u64) -> FaultResult<Self> {
        Self::with_backend(
            BulkBackend::default(),
            maximum_segments,
            maximum_bytes_per_segment,
        )
    }

    pub fn with_backend(
        backend: BulkBackend,
        maximum_segments: usize,
        maximum_bytes_per_segment: u64,
    ) -> FaultResult<Self> {
        if maximum_segments == 0 || maximum_bytes_per_segment == 0 {
            return Err(Fault::invalid_argument(
                "bulk-buffer limits must be positive",
            ));
        }
        if let BulkBackend::LocalFile(directory) = &backend {
            validate_directory(directory)?;
        }
        Ok(Self {
            backend,
            maximum_segments,
            maximum_bytes_per_segment,
            segments: Mutex::new(BTreeMap::new()),
        })
    }

    #[allow(clippy::too_many_arguments)]
    pub fn publish(
        &self,
        name: &str,
        bytes: &[u8],
        generation: u64,
        owner_process: &str,
        lease_expires_unix_millis: u64,
        now_unix_millis: u64,
    ) -> FaultResult<BufferDescriptor> {
        let length = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "bulk-buffer length exceeds u64"))?;
        if length == 0 || length > self.maximum_bytes_per_segment {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "bulk buffer exceeds per-segment limit",
            ));
        }
        if generation == 0 || lease_expires_unix_millis <= now_unix_millis {
            return Err(Fault::invalid_argument(
                "bulk-buffer generation or lease is invalid",
            ));
        }

        // Hold the registry lock while creating the OS object.  Segment
        // creation is local and bounded; doing so closes the admission race in
        // which concurrent publishers could all observe spare capacity.
        let mut segments = self
            .segments
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if segments.len() >= self.maximum_segments {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "bulk-buffer segment limit reached",
            ));
        }

        let segment = self.create_segment(
            name,
            bytes,
            generation,
            owner_process,
            lease_expires_unix_millis,
            now_unix_millis,
        )?;
        let descriptor = segment.descriptor().clone();
        if segments.contains_key(&descriptor.segment_id) {
            return Err(Fault::new(
                Code::AlreadyExists,
                "bulk-buffer segment id already exists",
            ));
        }
        segments.insert(descriptor.segment_id.clone(), segment);
        Ok(descriptor)
    }

    pub fn read_verified(
        &self,
        segment_id: &str,
        maximum_bytes: u64,
        now_unix_millis: u64,
    ) -> FaultResult<Vec<u8>> {
        if maximum_bytes == 0 || maximum_bytes > self.maximum_bytes_per_segment {
            return Err(Fault::invalid_argument(
                "bulk-buffer read limit is invalid",
            ));
        }
        let segment = self
            .segments
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .get(segment_id)
            .cloned()
            .ok_or_else(|| Fault::new(Code::NotFound, "bulk-buffer segment was not found"))?;
        segment.read_verified(maximum_bytes, now_unix_millis)
    }

    pub fn release(&self, segment_id: &str) -> bool {
        self.segments
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .remove(segment_id)
            .is_some()
    }

    /// Removes expired segments and returns the number released.
    pub fn reap_expired(&self, now_unix_millis: u64) -> usize {
        let mut segments = self
            .segments
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let before = segments.len();
        segments.retain(|_, segment| {
            segment.descriptor().lease_expires_unix_millis > now_unix_millis
        });
        before - segments.len()
    }

    #[must_use]
    pub fn active(&self) -> usize {
        self.segments
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .len()
    }

    #[must_use]
    pub const fn maximum_segments(&self) -> usize {
        self.maximum_segments
    }

    #[must_use]
    pub const fn maximum_bytes_per_segment(&self) -> u64 {
        self.maximum_bytes_per_segment
    }

    #[allow(clippy::too_many_arguments)]
    fn create_segment(
        &self,
        name: &str,
        bytes: &[u8],
        generation: u64,
        owner_process: &str,
        lease_expires_unix_millis: u64,
        now_unix_millis: u64,
    ) -> FaultResult<Arc<dyn BulkSegment>> {
        match &self.backend {
            BulkBackend::Memfd => create_memfd(
                name,
                bytes,
                generation,
                owner_process,
                lease_expires_unix_millis,
                now_unix_millis,
            ),
            BulkBackend::LocalFile(directory) => Ok(Arc::new(FileSegment::create(
                directory,
                owner_process,
                generation,
                bytes,
                lease_expires_unix_millis,
            )?)),
        }
    }
}

#[cfg(target_os = "linux")]
#[allow(clippy::too_many_arguments)]
fn create_memfd(
    name: &str,
    bytes: &[u8],
    generation: u64,
    owner_process: &str,
    lease_expires_unix_millis: u64,
    now_unix_millis: u64,
) -> FaultResult<Arc<dyn BulkSegment>> {
    Ok(Arc::new(MemfdSegment::create(
        name,
        bytes,
        generation,
        owner_process,
        lease_expires_unix_millis,
        now_unix_millis,
    )?))
}

#[cfg(not(target_os = "linux"))]
#[allow(clippy::too_many_arguments)]
fn create_memfd(
    _name: &str,
    _bytes: &[u8],
    _generation: u64,
    _owner_process: &str,
    _lease_expires_unix_millis: u64,
    _now_unix_millis: u64,
) -> FaultResult<Arc<dyn BulkSegment>> {
    Err(Fault::new(
        Code::Unimplemented,
        "memfd bulk IPC requires Linux; configure the local-file backend",
    ))
}

fn validate_directory(directory: &Path) -> FaultResult<()> {
    if directory.as_os_str().is_empty() {
        return Err(Fault::invalid_argument(
            "bulk-buffer directory is empty",
        ));
    }
    if !directory.is_absolute() {
        return Err(Fault::invalid_argument(
            "bulk-buffer directory must be absolute",
        ));
    }
    Ok(())
}
