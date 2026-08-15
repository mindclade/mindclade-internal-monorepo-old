// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Digest-aware readers and writers.

use crate::{Digest, Sha256};
use mindclade_faults::{Fault, FaultResult};
use std::io::{Read, Write};

pub fn hash_reader(mut reader: impl Read) -> FaultResult<Digest> {
    let mut state = Sha256::new();
    let mut buffer = [0_u8; 64 * 1024];
    loop {
        let read = reader
            .read(&mut buffer)
            .map_err(|error| Fault::internal("digest read failed").with_source(error))?;
        if read == 0 {
            return Ok(state.finalize());
        }
        state.update(&buffer[..read]);
    }
}

/// Writer that returns the digest and byte count of successfully written bytes.
pub struct DigestingWriter<W> {
    inner: W,
    state: Sha256,
    bytes_written: u64,
}

impl<W> DigestingWriter<W> {
    #[must_use]
    pub fn new(inner: W) -> Self {
        Self {
            inner,
            state: Sha256::new(),
            bytes_written: 0,
        }
    }
    #[must_use]
    pub fn finish(self) -> (W, Digest, u64) {
        (self.inner, self.state.finalize(), self.bytes_written)
    }
}

impl<W: Write> Write for DigestingWriter<W> {
    fn write(&mut self, buffer: &[u8]) -> std::io::Result<usize> {
        let written = self.inner.write(buffer)?;
        let written_bytes = u64::try_from(written).map_err(|_| {
            std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                "digest byte count exceeds u64",
            )
        })?;
        let next = self
            .bytes_written
            .checked_add(written_bytes)
            .ok_or_else(|| {
                std::io::Error::new(
                    std::io::ErrorKind::InvalidData,
                    "digest byte count overflow",
                )
            })?;
        self.state.update(&buffer[..written]);
        self.bytes_written = next;
        Ok(written)
    }
    fn flush(&mut self) -> std::io::Result<()> {
        self.inner.flush()
    }
}

/// Reader that verifies the full byte stream at EOF.
pub struct VerifyingReader<R> {
    inner: R,
    expected: Digest,
    state: Option<Sha256>,
    verified: bool,
}

impl<R> VerifyingReader<R> {
    #[must_use]
    pub fn new(inner: R, expected: Digest) -> Self {
        Self {
            inner,
            expected,
            state: Some(Sha256::new()),
            verified: false,
        }
    }
    #[must_use]
    pub const fn is_verified(&self) -> bool {
        self.verified
    }
}

impl<R: Read> Read for VerifyingReader<R> {
    fn read(&mut self, buffer: &mut [u8]) -> std::io::Result<usize> {
        let read = self.inner.read(buffer)?;
        if read > 0 {
            if let Some(state) = &mut self.state {
                state.update(&buffer[..read]);
            }
        } else if !self.verified {
            let actual = self
                .state
                .take()
                .map(Sha256::finalize)
                .unwrap_or(Digest::ZERO);
            if !actual.constant_time_eq(self.expected) {
                return Err(std::io::Error::new(
                    std::io::ErrorKind::InvalidData,
                    "content digest mismatch",
                ));
            }
            self.verified = true;
        }
        Ok(read)
    }
}
