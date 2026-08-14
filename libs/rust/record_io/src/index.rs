//! Monotonic record-frame index.

use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RecordIndexEntry {
    pub ordinal: u64,
    pub offset: u64,
    pub frame_bytes: u64,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct RecordIndex {
    entries: Vec<RecordIndexEntry>,
}

impl RecordIndex {
    pub fn push(&mut self, entry: RecordIndexEntry) -> FaultResult<()> {
        if entry.frame_bytes == 0 {
            return Err(Fault::invalid_argument("record index frame length must be non-zero"));
        }
        if let Some(last) = self.entries.last() {
            let expected_ordinal = last.ordinal.checked_add(1)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "record ordinal domain exhausted"))?;
            let minimum_offset = last.offset.checked_add(last.frame_bytes)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "record frame offset overflow"))?;
            if entry.ordinal != expected_ordinal || entry.offset < minimum_offset {
                return Err(Fault::new(Code::Conflict, "record index is not monotonic"));
            }
        } else if entry.ordinal != 0 {
            return Err(Fault::new(Code::Conflict, "record index must begin at ordinal zero"));
        }
        self.entries.push(entry);
        Ok(())
    }
    #[must_use]
    pub fn get(&self, ordinal: u64) -> Option<RecordIndexEntry> {
        usize::try_from(ordinal).ok().and_then(|index| self.entries.get(index).copied())
    }
    #[must_use] pub fn len(&self) -> usize { self.entries.len() }
    #[must_use] pub fn is_empty(&self) -> bool { self.entries.is_empty() }
}
