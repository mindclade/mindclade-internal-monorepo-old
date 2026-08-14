//! Validated HTTP/object range semantics independent of network framework.
use mindclade_bytes_io::ByteRange;
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RangeRequest { pub offset: u64, pub length: u64 }
impl RangeRequest {
    pub fn validate(self, object_size: u64, maximum_range_bytes: u64) -> FaultResult<ByteRange> {
        if self.length == 0 || self.length > maximum_range_bytes {
            return Err(Fault::new(Code::ResourceExhausted, "artifact range length is invalid or exceeds limit"));
        }
        let end = self.offset.checked_add(self.length)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "artifact range overflow"))?;
        if end > object_size {
            return Err(Fault::new(Code::OutOfRange, "artifact range exceeds object bounds"));
        }
        ByteRange::new(self.offset, self.length)
    }
}
