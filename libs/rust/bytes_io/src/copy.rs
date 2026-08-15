// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Code, Fault, FaultResult};
use std::io::{Read, Write};

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CopyReport {
    pub bytes: u64,
    pub operations: u64,
}

pub fn copy_bounded(
    mut reader: impl Read,
    mut writer: impl Write,
    maximum: u64,
    buffer_bytes: usize,
) -> FaultResult<CopyReport> {
    if buffer_bytes == 0 {
        return Err(Fault::invalid_argument("copy buffer must be non-zero"));
    }
    let mut buffer = vec![0_u8; buffer_bytes.min(8 * 1024 * 1024)];
    let mut report = CopyReport::default();
    loop {
        let read = reader
            .read(&mut buffer)
            .map_err(|e| Fault::internal("byte read failed").with_source(e))?;
        if read == 0 {
            break;
        }
        let read_bytes = u64::try_from(read)
            .map_err(|_| Fault::new(Code::OutOfRange, "copy read count exceeds u64"))?;
        report.bytes = report
            .bytes
            .checked_add(read_bytes)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "copy byte count overflow"))?;
        if report.bytes > maximum {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "copy exceeds byte limit",
            ));
        }
        writer
            .write_all(&buffer[..read])
            .map_err(|e| Fault::internal("byte write failed").with_source(e))?;
        report.operations += 1;
    }
    Ok(report)
}
