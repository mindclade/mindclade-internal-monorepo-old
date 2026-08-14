//! Compression contract. Core record framing is compression-agnostic; provider-specific codecs
//! are leaf adapters so the digest always covers the canonical uncompressed payload.
use mindclade_faults::{Code, Fault, FaultResult};

pub trait CompressionCodec: Send + Sync {
    fn name(&self) -> &'static str;
    fn compress(&self, input: &[u8]) -> FaultResult<Vec<u8>>;
    fn decompress(&self, input: &[u8], maximum_output: u64) -> FaultResult<Vec<u8>>;
}

#[derive(Clone, Copy, Debug, Default)]
pub struct IdentityCodec;
impl CompressionCodec for IdentityCodec {
    fn name(&self) -> &'static str { "identity" }
    fn compress(&self, input: &[u8]) -> FaultResult<Vec<u8>> { Ok(input.to_vec()) }
    fn decompress(&self, input: &[u8], maximum_output: u64) -> FaultResult<Vec<u8>> {
        let size = u64::try_from(input.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "decompressed byte count exceeds u64"))?;
        if size > maximum_output {
            return Err(Fault::new(Code::ResourceExhausted, "decompressed payload exceeds limit"));
        }
        Ok(input.to_vec())
    }
}
