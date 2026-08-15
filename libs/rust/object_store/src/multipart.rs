// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic multipart assembly with digest, order, and byte-budget checks.

use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Part {
    pub number: u32,
    pub bytes: Vec<u8>,
    pub digest: Digest,
}

impl Part {
    pub fn new(number: u32, bytes: Vec<u8>) -> FaultResult<Self> {
        if number == 0 {
            return Err(Fault::invalid_argument(
                "multipart part number must be non-zero",
            ));
        }
        let digest = hash_bytes(&bytes);
        Ok(Self {
            number,
            bytes,
            digest,
        })
    }
}

pub fn assemble(mut parts: Vec<Part>, maximum: u64) -> FaultResult<Vec<u8>> {
    if parts.is_empty() {
        return Err(Fault::invalid_argument("multipart upload has no parts"));
    }
    parts.sort_by_key(|part| part.number);
    let mut total = 0_u64;
    for (index, part) in parts.iter().enumerate() {
        let expected_number = u32::try_from(
            index
                .checked_add(1)
                .ok_or_else(|| Fault::new(Code::OutOfRange, "multipart part index overflow"))?,
        )
        .map_err(|_| Fault::new(Code::ResourceExhausted, "multipart part count exceeds u32"))?;
        if part.number != expected_number {
            return Err(Fault::data_loss(
                "multipart part sequence is not contiguous",
            ));
        }
        if hash_bytes(&part.bytes) != part.digest {
            return Err(Fault::data_loss("multipart part digest mismatch"));
        }
        let part_bytes = u64::try_from(part.bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "multipart part size exceeds u64"))?;
        total = total
            .checked_add(part_bytes)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "multipart size overflow"))?;
        if total > maximum {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "multipart object exceeds limit",
            ));
        }
    }
    let capacity = usize::try_from(total).map_err(|_| {
        Fault::new(
            Code::ResourceExhausted,
            "multipart object exceeds addressable memory",
        )
    })?;
    let mut output = Vec::with_capacity(capacity);
    for part in parts {
        output.extend_from_slice(&part.bytes);
    }
    Ok(output)
}
