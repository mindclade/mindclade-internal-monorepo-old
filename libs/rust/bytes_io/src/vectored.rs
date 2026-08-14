//! Bounded vectored writes without flattening or hidden whole-message copies.

use crate::CopyReport;
use mindclade_faults::{Code, Fault, FaultResult};
use std::io::{IoSlice, Write};

pub fn write_vectored_bounded<W: Write>(
    writer: &mut W,
    slices: &[IoSlice<'_>],
    maximum: u64,
) -> FaultResult<CopyReport> {
    let mut requested = 0_u64;
    for slice in slices {
        let length = u64::try_from(slice.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "vectored slice length exceeds u64"))?;
        requested = requested.checked_add(length)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "vectored length overflow"))?;
        if requested > maximum {
            return Err(Fault::new(Code::ResourceExhausted, "vectored write exceeds byte bound"));
        }
    }
    let mut operations = 0_u64;
    for slice in slices {
        writer.write_all(slice.as_ref())
            .map_err(|error| Fault::new(Code::Unavailable, "vectored write failed").with_source(error))?;
        if !slice.is_empty() {
            operations = operations.checked_add(1)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "vectored operation count overflow"))?;
        }
    }
    Ok(CopyReport { bytes: requested, operations })
}
