// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Validated allocation requests and their RAII reservation.
use super::{Budget, Reservation, ResourceVector};
use mindclade_faults::{Fault, FaultResult};
use std::sync::Arc;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AllocationRequest {
    pub owner: String,
    pub resources: ResourceVector,
}

impl AllocationRequest {
    pub fn validate(&self) -> FaultResult<()> {
        if self.owner.trim().is_empty() || self.owner.len() > 256 {
            return Err(Fault::invalid_argument("allocation owner is invalid"));
        }
        Ok(())
    }
}

#[derive(Debug)]
pub struct Allocation {
    pub owner: String,
    reservation: Reservation,
}

impl Allocation {
    pub fn acquire(budget: &Arc<Budget>, request: AllocationRequest) -> FaultResult<Self> {
        request.validate()?;
        Ok(Self {
            owner: request.owner,
            reservation: budget.reserve(request.resources)?,
        })
    }
    #[must_use]
    pub fn reservation(&self) -> &Reservation {
        &self.reservation
    }
}
