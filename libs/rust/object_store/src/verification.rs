// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Content-verification helpers used at object-store trust boundaries.

use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::{Fault, FaultResult};

pub fn verify_bytes(expected: Digest, bytes: &[u8]) -> FaultResult<()> {
    let actual = hash_bytes(bytes);
    if !actual.constant_time_eq(expected) {
        return Err(Fault::data_loss("object digest mismatch"));
    }
    Ok(())
}
