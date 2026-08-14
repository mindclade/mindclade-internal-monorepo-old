use mindclade_bytes_io::ByteSize;
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Limits {
    pub maximum_input_bytes: ByteSize,
    pub maximum_line_bytes: usize,
    pub maximum_records: usize,
    pub maximum_tokens: usize,
    pub maximum_metadata_entries: usize,
    pub maximum_nesting: usize,
    pub maximum_allocation_bytes: ByteSize,
}

impl Default for Limits {
    fn default() -> Self {
        Self {
            maximum_input_bytes: ByteSize::new(64 * 1024 * 1024),
            maximum_line_bytes: 1 << 20,
            maximum_records: 1_000_000,
            maximum_tokens: 10_000_000,
            maximum_metadata_entries: 1024,
            maximum_nesting: 64,
            maximum_allocation_bytes: ByteSize::new(256 * 1024 * 1024),
        }
    }
}

impl Limits {
    pub fn validate(self) -> FaultResult<Self> {
        if self.maximum_input_bytes.get() == 0
            || self.maximum_line_bytes == 0
            || self.maximum_records == 0
            || self.maximum_tokens == 0
            || self.maximum_metadata_entries == 0
            || self.maximum_nesting == 0
            || self.maximum_allocation_bytes.get() == 0
        {
            return Err(Fault::invalid_argument("parse limits must be positive"));
        }
        let maximum_reasonable_input = self
            .maximum_allocation_bytes
            .get()
            .checked_mul(4)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "parse allocation/input ratio overflows u64"))?;
        if self.maximum_input_bytes.get() > maximum_reasonable_input {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "input/allocation limits are inconsistent",
            ));
        }
        Ok(self)
    }
}
