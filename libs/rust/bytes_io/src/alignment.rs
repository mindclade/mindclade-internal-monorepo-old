pub use crate::spec::Alignment;
pub fn align_up(value:u64,alignment:Alignment)->mindclade_faults::FaultResult<u64>{alignment.align_up(value)}
