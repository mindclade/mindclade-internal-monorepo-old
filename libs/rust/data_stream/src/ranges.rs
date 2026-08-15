// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::Shard;
use mindclade_bytes_io::ByteRange;
use mindclade_faults::{Fault, FaultResult};

pub fn shard_range(shard: &Shard, offset: u64, length: u64) -> FaultResult<ByteRange> {
    let range = ByteRange::new(offset, length)?;
    if range.end() > shard.size {
        return Err(Fault::invalid_argument("shard range exceeds object"));
    }
    Ok(range)
}
