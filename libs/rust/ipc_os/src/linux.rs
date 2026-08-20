// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::BulkSegment;
use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::{BufferAccess, BufferDescriptor, BufferTransport};
use std::ffi::CString;
use std::fs::File;
use std::io::{Read, Seek, SeekFrom, Write};
use std::os::fd::{AsRawFd, FromRawFd, RawFd};

const MAX_SEGMENT_BYTES: u64 = 16 * 1024 * 1024 * 1024;

#[derive(Debug)]
pub struct MemfdSegment {
    file: File,
    descriptor: BufferDescriptor,
}

impl MemfdSegment {
    #[allow(clippy::too_many_arguments)]
    pub fn create(
        name: &str,
        bytes: &[u8],
        generation: u64,
        owner_process: &str,
        lease_expires_unix_millis: u64,
        now_unix_millis: u64,
    ) -> FaultResult<Self> {
        if name.is_empty()
            || name.len() > 128
            || owner_process.is_empty()
            || owner_process.len() > 256
        {
            return Err(Fault::invalid_argument("memfd name or owner is invalid"));
        }
        let length = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "memfd payload length exceeds u64"))?;
        if length == 0 || length > MAX_SEGMENT_BYTES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "memfd payload is outside bulk IPC bounds",
            ));
        }
        let c_name =
            CString::new(name).map_err(|_| Fault::invalid_argument("memfd name contains NUL"))?;
        // SAFETY: `c_name` is a live NUL-terminated C string.  A non-negative
        // descriptor returned by the kernel is transferred exactly once into
        // `File::from_raw_fd` below and thereafter owned by RAII.
        let fd = unsafe {
            libc::memfd_create(c_name.as_ptr(), libc::MFD_CLOEXEC | libc::MFD_ALLOW_SEALING)
        };
        if fd < 0 {
            return Err(Fault::new(Code::Unavailable, "memfd_create failed")
                .with_source(std::io::Error::last_os_error()));
        }
        // SAFETY: `fd` was just created successfully and is not owned elsewhere.
        let mut file = unsafe { File::from_raw_fd(fd) };
        file.write_all(bytes)
            .and_then(|()| file.flush())
            .and_then(|()| file.seek(SeekFrom::Start(0)).map(|_| ()))
            .map_err(|error| {
                Fault::new(Code::Unavailable, "failed to initialize memfd segment")
                    .with_source(error)
            })?;
        let seals =
            libc::F_SEAL_WRITE | libc::F_SEAL_GROW | libc::F_SEAL_SHRINK | libc::F_SEAL_SEAL;
        // SAFETY: `file` owns a live memfd created with `MFD_ALLOW_SEALING`;
        // `seals` contains only Linux memfd seal flags.
        if unsafe { libc::fcntl(file.as_raw_fd(), libc::F_ADD_SEALS, seals) } < 0 {
            return Err(
                Fault::new(Code::Unavailable, "failed to seal memfd segment")
                    .with_source(std::io::Error::last_os_error()),
            );
        }
        let descriptor = BufferDescriptor {
            segment_id: format!("memfd:{name}:{generation}"),
            generation,
            range: ByteRange::new(0, length)?,
            element_type: "bytes".into(),
            shape: vec![length],
            digest: hash_bytes(bytes),
            owner_process: owner_process.to_owned(),
            lease_expires_unix_millis,
            access: BufferAccess::ReadOnly,
            transport: BufferTransport::FileDescriptor,
            locator: format!("fd:{}", file.as_raw_fd()),
        };
        descriptor.validate(now_unix_millis)?;
        Ok(Self { file, descriptor })
    }
    #[must_use]
    pub fn raw_fd(&self) -> RawFd {
        self.file.as_raw_fd()
    }
    /// Makes this descriptor inheritable across the next process spawn.  Call
    /// `set_close_on_exec` immediately after spawning the intended worker.
    pub fn set_inheritable(&self) -> FaultResult<()> {
        self.set_fd_flags(false)
    }
    pub fn set_close_on_exec(&self) -> FaultResult<()> {
        self.set_fd_flags(true)
    }
    fn set_fd_flags(&self, close_on_exec: bool) -> FaultResult<()> {
        let fd = self.file.as_raw_fd();
        // SAFETY: `fd` is owned by `self.file` and remains live for this call.
        let current = unsafe { libc::fcntl(fd, libc::F_GETFD) };
        if current < 0 {
            return Err(
                Fault::new(Code::Unavailable, "failed to read file-descriptor flags")
                    .with_source(std::io::Error::last_os_error()),
            );
        }
        let updated = if close_on_exec {
            current | libc::FD_CLOEXEC
        } else {
            current & !libc::FD_CLOEXEC
        };
        // SAFETY: `fd` is valid and `updated` contains only descriptor flags.
        let result = unsafe { libc::fcntl(fd, libc::F_SETFD, updated) };
        if result < 0 {
            return Err(
                Fault::new(Code::Unavailable, "failed to update file-descriptor flags")
                    .with_source(std::io::Error::last_os_error()),
            );
        }
        Ok(())
    }
}

impl BulkSegment for MemfdSegment {
    fn descriptor(&self) -> &BufferDescriptor {
        &self.descriptor
    }
    fn read_verified(&self, maximum_bytes: u64, now_unix_millis: u64) -> FaultResult<Vec<u8>> {
        self.descriptor.validate(now_unix_millis)?;
        if self.descriptor.range.length() > maximum_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "bulk segment exceeds read budget",
            ));
        }
        let mut file = self.file.try_clone().map_err(|error| {
            Fault::new(Code::Unavailable, "failed to clone memfd descriptor").with_source(error)
        })?;
        file.seek(SeekFrom::Start(self.descriptor.range.start()))
            .map_err(|error| {
                Fault::new(Code::Unavailable, "failed to seek memfd segment").with_source(error)
            })?;
        let capacity = usize::try_from(self.descriptor.range.length()).map_err(|_| {
            Fault::new(
                Code::OutOfRange,
                "bulk segment length exceeds platform usize",
            )
        })?;
        let mut bytes = Vec::with_capacity(capacity);
        let mut limited = file.take(self.descriptor.range.length());
        limited.read_to_end(&mut bytes).map_err(|error| {
            Fault::new(Code::Unavailable, "failed to read memfd segment").with_source(error)
        })?;
        if bytes.len() != capacity {
            return Err(Fault::data_loss("bulk segment was truncated"));
        }
        self.descriptor.digest.verify(&bytes)?;
        Ok(bytes)
    }
}
