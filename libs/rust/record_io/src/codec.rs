// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Length-delimited canonical primitives.

use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Debug, Default)]
pub struct Encoder {
    bytes: Vec<u8>,
}

impl Encoder {
    #[must_use]
    pub const fn new() -> Self {
        Self { bytes: Vec::new() }
    }
    #[must_use]
    pub fn with_capacity(capacity: usize) -> Self {
        Self {
            bytes: Vec::with_capacity(capacity),
        }
    }
    #[must_use]
    pub fn into_bytes(self) -> Vec<u8> {
        self.bytes
    }
    #[must_use]
    pub fn len(&self) -> usize {
        self.bytes.len()
    }
    pub fn u8(&mut self, value: u8) {
        self.bytes.push(value);
    }
    pub fn u16(&mut self, value: u16) {
        self.bytes.extend_from_slice(&value.to_be_bytes());
    }
    pub fn u32(&mut self, value: u32) {
        self.bytes.extend_from_slice(&value.to_be_bytes());
    }
    pub fn u64(&mut self, value: u64) {
        self.bytes.extend_from_slice(&value.to_be_bytes());
    }
    pub fn bool(&mut self, value: bool) {
        self.u8(u8::from(value));
    }
    pub fn bytes(&mut self, value: &[u8]) -> FaultResult<()> {
        let length = u32::try_from(value.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "encoded byte string exceeds u32"))?;
        self.u32(length);
        self.bytes.extend_from_slice(value);
        Ok(())
    }
    pub fn string(&mut self, value: &str) -> FaultResult<()> {
        self.bytes(value.as_bytes())
    }
}

#[derive(Clone, Debug)]
pub struct Decoder<'a> {
    bytes: &'a [u8],
    offset: usize,
    max_field_bytes: usize,
    max_items: usize,
}

impl<'a> Decoder<'a> {
    pub fn new(bytes: &'a [u8], max_message_bytes: usize) -> FaultResult<Self> {
        if bytes.len() > max_message_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "encoded message exceeds limit",
            ));
        }
        Ok(Self {
            bytes,
            offset: 0,
            max_field_bytes: max_message_bytes,
            max_items: 1_000_000,
        })
    }
    #[must_use]
    pub fn remaining(&self) -> usize {
        self.bytes.len().saturating_sub(self.offset)
    }
    #[must_use]
    pub const fn max_items(&self) -> usize {
        self.max_items
    }
    pub fn finish(self) -> FaultResult<()> {
        if self.offset == self.bytes.len() {
            Ok(())
        } else {
            Err(Fault::data_loss("encoded message contains trailing bytes"))
        }
    }
    fn take(&mut self, length: usize) -> FaultResult<&'a [u8]> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or_else(|| Fault::data_loss("encoded field length overflow"))?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or_else(|| Fault::data_loss("encoded message is truncated"))?;
        self.offset = end;
        Ok(value)
    }
    pub fn u8(&mut self) -> FaultResult<u8> {
        Ok(self.take(1)?[0])
    }
    pub fn u16(&mut self) -> FaultResult<u16> {
        let b = self.take(2)?;
        Ok(u16::from_be_bytes([b[0], b[1]]))
    }
    pub fn u32(&mut self) -> FaultResult<u32> {
        let b = self.take(4)?;
        Ok(u32::from_be_bytes([b[0], b[1], b[2], b[3]]))
    }
    pub fn u64(&mut self) -> FaultResult<u64> {
        let b = self.take(8)?;
        Ok(u64::from_be_bytes([
            b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
        ]))
    }
    pub fn bool(&mut self) -> FaultResult<bool> {
        match self.u8()? {
            0 => Ok(false),
            1 => Ok(true),
            _ => Err(Fault::data_loss("encoded boolean is invalid")),
        }
    }
    pub fn bytes(&mut self) -> FaultResult<&'a [u8]> {
        let length = usize::try_from(self.u32()?)
            .map_err(|_| Fault::data_loss("encoded field length is invalid"))?;
        if length > self.max_field_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "encoded field exceeds limit",
            ));
        }
        self.take(length)
    }
    pub fn string(&mut self) -> FaultResult<&'a str> {
        std::str::from_utf8(self.bytes()?)
            .map_err(|error| Fault::data_loss("encoded string is not UTF-8").with_source(error))
    }
    pub fn item_count(&mut self) -> FaultResult<usize> {
        let count = usize::try_from(self.u32()?)
            .map_err(|_| Fault::data_loss("encoded item count is invalid"))?;
        if count > self.max_items {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "encoded item count exceeds limit",
            ));
        }
        Ok(count)
    }
}
