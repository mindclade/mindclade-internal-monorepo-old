// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Shared policy-floor validation for runtime consumers.

use crate::{RevocationSnapshot, SignatureVerifier};
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PolicyFloor {
    pub minimum_policy_epoch: u64,
    pub minimum_route_version: u64,
    pub minimum_revocation_epoch: u64,
}

impl PolicyFloor {
    pub fn validate(self) -> FaultResult<Self> {
        if self.minimum_policy_epoch == 0
            || self.minimum_route_version == 0
            || self.minimum_revocation_epoch == 0
        {
            return Err(Fault::invalid_argument(
                "policy floor values must be non-zero",
            ));
        }
        Ok(self)
    }
    pub fn validate_revocations<V: SignatureVerifier + ?Sized>(
        self,
        snapshot: &RevocationSnapshot,
        now: u64,
        verifier: &V,
    ) -> FaultResult<()> {
        self.validate()?;
        snapshot
            .validate(now, self.minimum_revocation_epoch, verifier)
            .map_err(|error| {
                Fault::new(Code::PermissionDenied, "revocation state is not acceptable")
                    .with_source(error)
            })
    }
}
