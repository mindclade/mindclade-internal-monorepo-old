// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Range-read policy enforcement.

use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RangePolicy {
    pub maximum_read: ByteSize,
}

impl RangePolicy {
    pub fn validate(self, range: ByteRange) -> FaultResult<()> {
        if self.maximum_read.get() == 0 {
            return Err(Fault::invalid_argument(
                "range policy maximum read must be non-zero",
            ));
        }
        if range.is_empty() {
            return Err(Fault::invalid_argument("range is empty"));
        }
        if range.length() > self.maximum_read.get() {
            return Err(Fault::new(Code::ResourceExhausted, "range exceeds policy"));
        }
        Ok(())
    }
}
