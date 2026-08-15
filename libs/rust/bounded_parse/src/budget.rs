// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Explicit retained-allocation accounting for untrusted parsers.
#![forbid(unsafe_code)]

use crate::Limits;
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Debug)]
pub struct AllocationBudget {
    limit: u64,
    used: u64,
}

impl AllocationBudget {
    #[must_use]
    pub fn from_limits(limits: Limits) -> Self {
        Self {
            limit: limits.maximum_allocation_bytes.get(),
            used: 0,
        }
    }
    pub fn charge(&mut self, bytes: u64) -> FaultResult<()> {
        let next = self
            .used
            .checked_add(bytes)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "parse allocation accounting overflow"))?;
        if next > self.limit {
            return Err(
                Fault::new(Code::ResourceExhausted, "parse allocation budget exceeded")
                    .with_context("used", self.used)
                    .with_context("requested", bytes)
                    .with_context("limit", self.limit),
            );
        }
        self.used = next;
        Ok(())
    }
    pub fn charge_usize(&mut self, bytes: usize) -> FaultResult<()> {
        let bytes = u64::try_from(bytes)
            .map_err(|_| Fault::new(Code::OutOfRange, "parse allocation length exceeds u64"))?;
        self.charge(bytes)
    }
    #[must_use]
    pub const fn used(&self) -> u64 {
        self.used
    }
    #[must_use]
    pub const fn limit(&self) -> u64 {
        self.limit
    }
    pub fn remaining(&self) -> FaultResult<u64> {
        self.limit
            .checked_sub(self.used)
            .ok_or_else(|| Fault::data_loss("parse allocation accounting exceeds configured limit"))
    }
}
