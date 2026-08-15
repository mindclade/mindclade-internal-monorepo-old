// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Portable bounded local-file fallback for bulk IPC.
//!
//! Linux production deployments should prefer `MemfdSegment`; this adapter is
//! used on platforms without memfd and for diagnostics/recovery paths.

use crate::BulkSegment;
use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::{BufferAccess, BufferDescriptor, BufferTransport};
use std::fs::{self, OpenOptions};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

static NEXT_SEGMENT: AtomicU64 = AtomicU64::new(1);
const MAX_SEGMENT_BYTES: u64 = 4 * 1024 * 1024 * 1024;

#[derive(Debug)]
pub struct FileSegment {
    path: PathBuf,
    descriptor: BufferDescriptor,
}

impl FileSegment {
    pub fn create(
        directory: &Path,
        owner_process: &str,
        generation: u64,
        bytes: &[u8],
        lease_expires_unix_millis: u64,
    ) -> FaultResult<Self> {
        if owner_process.is_empty() || owner_process.len() > 256 || generation == 0 {
            return Err(Fault::invalid_argument(
                "portable IPC segment metadata is invalid",
            ));
        }
        let length = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "portable IPC segment length exceeds u64"))?;
        if length == 0 || length > MAX_SEGMENT_BYTES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "portable IPC segment exceeds byte limit",
            ));
        }
        fs::create_dir_all(directory).map_err(|error| {
            Fault::new(Code::Unavailable, "failed to create portable IPC directory")
                .with_source(error)
        })?;
        let sequence = NEXT_SEGMENT
            .fetch_update(Ordering::SeqCst, Ordering::SeqCst, |current| {
                current.checked_add(1)
            })
            .map_err(|_| Fault::new(Code::OutOfRange, "portable IPC segment counter exhausted"))?;
        let file_name = format!("mindclade-{generation}-{sequence}.buffer");
        let final_path = directory.join(&file_name);
        let temp_path = directory.join(format!(".{file_name}.tmp"));
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&temp_path)
            .map_err(|error| {
                Fault::new(Code::Unavailable, "failed to create portable IPC segment")
                    .with_source(error)
            })?;
        if let Err(error) = file.write_all(bytes).and_then(|()| file.sync_all()) {
            let _ = fs::remove_file(&temp_path);
            return Err(
                Fault::new(Code::Unavailable, "failed to write portable IPC segment")
                    .with_source(error),
            );
        }
        drop(file);
        fs::rename(&temp_path, &final_path).map_err(|error| {
            let _ = fs::remove_file(&temp_path);
            Fault::new(Code::Unavailable, "failed to publish portable IPC segment")
                .with_source(error)
        })?;
        let digest = hash_bytes(bytes);
        let descriptor = BufferDescriptor {
            segment_id: file_name,
            generation,
            range: ByteRange::new(0, length)?,
            element_type: "u8".to_owned(),
            shape: vec![length],
            digest,
            owner_process: owner_process.to_owned(),
            lease_expires_unix_millis,
            access: BufferAccess::ReadOnly,
            transport: BufferTransport::LocalFile,
            locator: final_path.to_string_lossy().into_owned(),
        };
        Ok(Self {
            path: final_path,
            descriptor,
        })
    }
    #[must_use]
    pub fn descriptor(&self) -> &BufferDescriptor {
        &self.descriptor
    }
    fn read_with_limit(&self, maximum_bytes: u64, now_unix_millis: u64) -> FaultResult<Vec<u8>> {
        self.descriptor.validate(now_unix_millis)?;
        if self.descriptor.range.length() > maximum_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "portable IPC segment exceeds read budget",
            ));
        }
        let file = OpenOptions::new()
            .read(true)
            .open(&self.path)
            .map_err(|error| {
                Fault::new(Code::Unavailable, "failed to open portable IPC segment")
                    .with_source(error)
            })?;
        let capacity = usize::try_from(self.descriptor.range.length()).map_err(|_| {
            Fault::new(
                Code::OutOfRange,
                "portable IPC segment length exceeds usize",
            )
        })?;
        let mut bytes = Vec::with_capacity(capacity);
        file.take(self.descriptor.range.length())
            .read_to_end(&mut bytes)
            .map_err(|error| {
                Fault::new(Code::Unavailable, "failed to read portable IPC segment")
                    .with_source(error)
            })?;
        if hash_bytes(&bytes) != self.descriptor.digest {
            return Err(Fault::data_loss("portable IPC segment digest mismatch"));
        }
        Ok(bytes)
    }
    pub fn read_verified(&self, now_unix_millis: u64) -> FaultResult<Vec<u8>> {
        self.read_with_limit(MAX_SEGMENT_BYTES, now_unix_millis)
    }
    pub fn remove(mut self) -> FaultResult<()> {
        let path = std::mem::take(&mut self.path);
        if path.as_os_str().is_empty() {
            return Ok(());
        }
        match fs::remove_file(path) {
            Ok(()) => Ok(()),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(error) => Err(Fault::new(
                Code::Unavailable,
                "failed to remove portable IPC segment",
            )
            .with_source(error)),
        }
    }
}

impl Drop for FileSegment {
    fn drop(&mut self) {
        if !self.path.as_os_str().is_empty() {
            let _ = fs::remove_file(&self.path);
        }
    }
}

impl BulkSegment for FileSegment {
    fn descriptor(&self) -> &BufferDescriptor {
        &self.descriptor
    }
    fn read_verified(&self, maximum_bytes: u64, now_unix_millis: u64) -> FaultResult<Vec<u8>> {
        self.read_with_limit(maximum_bytes, now_unix_millis)
    }
}
