// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use super::record::SdfRecord;
use mindclade_faults::FaultResult;

pub fn serialize(records: &[SdfRecord]) -> FaultResult<Vec<u8>> {
    let mut output = Vec::new();
    for record in records {
        record.validate()?;
        output.extend_from_slice(&record.bytes);
        if !output.ends_with(b"\n") {
            output.push(b'\n');
        }
        output.extend_from_slice(b"$$$$\n");
    }
    Ok(output)
}
