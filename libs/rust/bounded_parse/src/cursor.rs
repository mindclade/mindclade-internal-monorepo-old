use crate::{Limits, Location, Source};
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Debug)]
pub struct Cursor<'a> {
    bytes: &'a [u8],
    position: usize,
    line: usize,
    limits: Limits,
}

impl<'a> Cursor<'a> {
    pub fn new(bytes: &'a [u8], limits: Limits) -> FaultResult<Self> {
        let source = Source::new(bytes, limits)?;
        Ok(Self {
            bytes: source.bytes(),
            position: 0,
            line: 1,
            limits: source.limits(),
        })
    }
    pub fn next_line(&mut self) -> FaultResult<Option<(Location, &'a [u8])>> {
        if self.position >= self.bytes.len() {
            return Ok(None);
        }
        let start = self.position;
        let end = self.bytes[start..]
            .iter()
            .position(|byte| *byte == b'\n')
            .and_then(|offset| start.checked_add(offset))
            .unwrap_or(self.bytes.len());
        let line_bytes = end
            .checked_sub(start)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "line offset accounting underflow"))?;
        if line_bytes > self.limits.maximum_line_bytes {
            let offset = u64::try_from(start)
                .map_err(|_| Fault::new(Code::OutOfRange, "parse byte offset exceeds u64"))?;
            return Err(Fault::new(Code::ResourceExhausted, "line exceeds parse limit")
                .with_context("offset", offset));
        }
        self.position = if end < self.bytes.len() {
            end.checked_add(1)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "parse cursor overflow"))?
        } else {
            end
        };
        let location = Location {
            offset: start,
            line: self.line,
            column: 1,
        };
        self.line = self
            .line
            .checked_add(1)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "parse line number overflow"))?;
        let mut slice = &self.bytes[start..end];
        if slice.last() == Some(&b'\r') {
            slice = &slice[..slice.len() - 1];
        }
        Ok(Some((location, slice)))
    }
    pub fn remaining(&self) -> FaultResult<usize> {
        self.bytes
            .len()
            .checked_sub(self.position)
            .ok_or_else(|| Fault::data_loss("parse cursor position exceeds input length"))
    }
    #[must_use]
    pub fn position(&self) -> usize {
        self.position
    }
}
