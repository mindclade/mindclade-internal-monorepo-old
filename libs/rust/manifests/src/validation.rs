// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Fault, FaultResult};

pub fn validate_schema_version(version: u32) -> FaultResult<()> {
    if version == 0 {
        return Err(Fault::invalid_argument("schema version must be non-zero"));
    }
    Ok(())
}

pub fn validate_kind(value: &str) -> FaultResult<()> {
    if value.is_empty()
        || value.len() > 128
        || !value.bytes().all(|b| {
            b.is_ascii_lowercase() || b.is_ascii_digit() || matches!(b, b'.' | b'_' | b'-' | b'/')
        })
    {
        return Err(Fault::invalid_argument("logical kind is invalid"));
    }
    Ok(())
}

pub fn validate_media_type(value: &str) -> FaultResult<()> {
    if value.is_empty() || value.len() > 256 || !value.is_ascii() {
        return Err(Fault::invalid_argument("media type is invalid"));
    }
    Ok(())
}
