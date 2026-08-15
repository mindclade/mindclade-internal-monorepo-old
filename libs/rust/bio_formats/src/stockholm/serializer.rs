// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::common::SequenceRecord;
use mindclade_faults::{
    Fault, FaultResult
};

pub fn serialize(records: &[SequenceRecord]) -> FaultResult<Vec<u8>> {
    let mut out=b"# STOCKHOLM 1.0\n".to_vec();
    for r in records {
        if r.id.is_empty() {
            return Err(Fault::invalid_argument("Stockholm id is empty"));
        }
        out.extend_from_slice(r.id.as_bytes());
        out.push(b' ');
        out.extend_from_slice(&r.sequence);
        out.push(b'\n');
    }
    out.extend_from_slice(b"//\n");
    Ok(out)
}
