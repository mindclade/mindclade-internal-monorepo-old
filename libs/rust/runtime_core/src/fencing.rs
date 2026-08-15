// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Monotonic fencing tokens used to reject stale workers and commits.

use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct FencingToken(u64);

impl FencingToken {
    pub fn new(value: u64) -> FaultResult<Self> {
        if value == 0 {
            return Err(Fault::invalid_argument("fencing token must be non-zero"));
        }
        Ok(Self(value))
    }
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }
    pub fn next(self) -> FaultResult<Self> {
        self.0
            .checked_add(1)
            .map(Self)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "fencing token overflow"))
    }
    pub fn require_current(self, current: Self) -> FaultResult<()> {
        if self == current {
            Ok(())
        } else {
            Err(Fault::new(Code::Conflict, "stale fencing token"))
        }
    }
}
