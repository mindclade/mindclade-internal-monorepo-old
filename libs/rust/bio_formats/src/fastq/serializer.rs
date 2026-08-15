// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::common::FastqRecord;
use mindclade_faults::{Fault, FaultResult};

pub fn serialize(records: &[FastqRecord]) -> FaultResult<Vec<u8>> {
    let mut output = Vec::new();
    for record in records {
        record.validate()?;
        if record
            .id
            .bytes()
            .any(|byte| byte.is_ascii_whitespace() || byte == b'@')
        {
            return Err(Fault::invalid_argument("FASTQ record id is not canonical"));
        }
        if record.description.contains('\n') || record.description.contains('\r') {
            return Err(Fault::invalid_argument(
                "FASTQ description contains newline",
            ));
        }
        output.push(b'@');
        output.extend_from_slice(record.id.as_bytes());
        if !record.description.is_empty() {
            output.push(b' ');
            output.extend_from_slice(record.description.as_bytes());
        }
        output.push(b'\n');
        output.extend_from_slice(&record.sequence);
        output.extend_from_slice(b"\n+\n");
        output.extend_from_slice(&record.quality);
        output.push(b'\n');
    }
    Ok(output)
}
