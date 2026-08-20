// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Integrity-checked record frames.

use mindclade_bytes_io::ByteSize;
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::{Code, Fault, FaultResult};
use std::io::{Read, Write};

pub const FRAME_MAGIC: [u8; 4] = *b"MCRD";
pub const FRAME_HEADER_BYTES: usize = 48;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Record {
    pub schema: u16,
    pub flags: u16,
    pub payload: Vec<u8>,
    pub digest: Digest,
}

#[derive(Debug)]
pub struct RecordWriter<W> {
    inner: W,
    bytes_written: u64,
}

impl<W: Write> RecordWriter<W> {
    pub fn new(inner: W) -> Self {
        Self {
            inner,
            bytes_written: 0,
        }
    }
    pub fn write(&mut self, schema: u16, flags: u16, payload: &[u8]) -> FaultResult<Digest> {
        let length = u64::try_from(payload.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "record payload is too large"))?;
        let digest = hash_bytes(payload);
        self.inner
            .write_all(&FRAME_MAGIC)
            .map_err(|error| Fault::internal("failed to write record magic").with_source(error))?;
        self.inner
            .write_all(&schema.to_be_bytes())
            .map_err(|error| Fault::internal("failed to write record schema").with_source(error))?;
        self.inner
            .write_all(&flags.to_be_bytes())
            .map_err(|error| Fault::internal("failed to write record flags").with_source(error))?;
        self.inner
            .write_all(&length.to_be_bytes())
            .map_err(|error| Fault::internal("failed to write record length").with_source(error))?;
        self.inner
            .write_all(digest.as_bytes())
            .map_err(|error| Fault::internal("failed to write record digest").with_source(error))?;
        self.inner.write_all(payload).map_err(|error| {
            Fault::internal("failed to write record payload").with_source(error)
        })?;
        let frame_bytes = 48_u64
            .checked_add(length)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "record frame length overflow"))?;
        self.bytes_written = self
            .bytes_written
            .checked_add(frame_bytes)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "record writer byte counter overflow"))?;
        Ok(digest)
    }
    pub fn flush(&mut self) -> FaultResult<()> {
        self.inner
            .flush()
            .map_err(|error| Fault::internal("failed to flush record writer").with_source(error))
    }
    #[must_use]
    pub const fn bytes_written(&self) -> u64 {
        self.bytes_written
    }
    #[must_use]
    pub fn into_inner(self) -> W {
        self.inner
    }
}

#[derive(Debug)]
pub struct RecordReader<R> {
    inner: R,
    maximum_payload: ByteSize,
}

impl<R: Read> RecordReader<R> {
    pub fn new(inner: R, maximum_payload: ByteSize) -> Self {
        Self {
            inner,
            maximum_payload,
        }
    }
    pub fn read_next(&mut self) -> FaultResult<Option<Record>> {
        let mut magic = [0_u8; 4];
        let first = self
            .inner
            .read(&mut magic[..1])
            .map_err(|error| Fault::internal("failed to read record").with_source(error))?;
        if first == 0 {
            return Ok(None);
        }
        self.inner
            .read_exact(&mut magic[1..])
            .map_err(|error| Fault::data_loss("record magic is truncated").with_source(error))?;
        if magic != FRAME_MAGIC {
            return Err(Fault::data_loss("record magic is invalid"));
        }
        let mut short = [0_u8; 2];
        self.inner
            .read_exact(&mut short)
            .map_err(|error| Fault::data_loss("record schema is truncated").with_source(error))?;
        let schema = u16::from_be_bytes(short);
        self.inner
            .read_exact(&mut short)
            .map_err(|error| Fault::data_loss("record flags are truncated").with_source(error))?;
        let flags = u16::from_be_bytes(short);
        let mut long = [0_u8; 8];
        self.inner
            .read_exact(&mut long)
            .map_err(|error| Fault::data_loss("record length is truncated").with_source(error))?;
        let length = u64::from_be_bytes(long);
        if length > self.maximum_payload.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "record payload exceeds configured limit",
            ));
        }
        let mut digest_bytes = [0_u8; 32];
        self.inner
            .read_exact(&mut digest_bytes)
            .map_err(|error| Fault::data_loss("record digest is truncated").with_source(error))?;
        let digest = Digest::from_bytes(digest_bytes);
        let allocation = usize::try_from(length)
            .map_err(|_| Fault::new(Code::OutOfRange, "record length exceeds platform limits"))?;
        let mut payload = vec![0_u8; allocation];
        self.inner
            .read_exact(&mut payload)
            .map_err(|error| Fault::data_loss("record payload is truncated").with_source(error))?;
        digest.verify(&payload)?;
        Ok(Some(Record {
            schema,
            flags,
            payload,
            digest,
        }))
    }
    #[must_use]
    pub fn into_inner(self) -> R {
        self.inner
    }
}
