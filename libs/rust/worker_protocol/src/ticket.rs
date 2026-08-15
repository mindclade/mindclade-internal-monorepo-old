// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

pub use crate::{
    AdmissionGrant, AdmissionGrantClaims, ArtifactGrant, DetachedSignature, ExecutionBudget,
    ExecutionTicket, ExecutionTicketClaims, HmacSha256Verifier, SignatureVerifier,
};
use crate::{RevocationSnapshot, validation::PolicyFloor};
use mindclade_faults::FaultResult;

pub fn validate_execution<V: SignatureVerifier + ?Sized>(
    ticket: &ExecutionTicket,
    now: u64,
    floor: PolicyFloor,
    revocations: &RevocationSnapshot,
    verifier: &V,
) -> FaultResult<()> {
    let floor = floor.validate()?;
    ticket.validate(
        now,
        floor.minimum_policy_epoch,
        floor.minimum_route_version,
        revocations,
        verifier,
    )
}
