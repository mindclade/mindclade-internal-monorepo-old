use crate::Limits;
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug)]
pub struct Source<'a> {
    bytes: &'a [u8],
    limits: Limits,
}

impl<'a> Source<'a> {
    pub fn new(bytes: &'a [u8], limits: Limits) -> FaultResult<Self> {
        let limits = limits.validate()?;
        let input_bytes = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "parse input length exceeds u64"))?;
        if input_bytes > limits.maximum_input_bytes.get() {
            return Err(Fault::new(Code::ResourceExhausted, "parse input exceeds limit"));
        }
        Ok(Self { bytes, limits })
    }
    #[must_use]
    pub fn bytes(self) -> &'a [u8] { self.bytes }
    #[must_use]
    pub const fn limits(self) -> Limits { self.limits }
}
