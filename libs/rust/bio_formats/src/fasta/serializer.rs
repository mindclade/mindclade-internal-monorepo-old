// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::common::SequenceRecord;
use mindclade_faults::{Fault, FaultResult};

pub fn serialize(records: &[SequenceRecord], line_width: usize) -> FaultResult<Vec<u8>> {
    if line_width == 0 || line_width > 1_000_000 {
        return Err(Fault::invalid_argument("FASTA line width is invalid"));
    }
    let mut output = Vec::new();
    for record in records {
        record.validate()?;
        if record.id.bytes().any(|byte| byte.is_ascii_whitespace() || byte == b'>') {
            return Err(Fault::invalid_argument("FASTA record id is not canonical"));
        }
        if record.description.contains('\n') || record.description.contains('\r') {
            return Err(Fault::invalid_argument("FASTA description contains newline"));
        }
        output.push(b'>');
        output.extend_from_slice(record.id.as_bytes());
        if !record.description.is_empty() {
            output.push(b' ');
            output.extend_from_slice(record.description.as_bytes());
        }
        output.push(b'\n');
        for chunk in record.sequence.chunks(line_width) {
            output.extend_from_slice(chunk);
            output.push(b'\n');
        }
    }
    Ok(output)
}
