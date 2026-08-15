// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic local route selection from a signed immutable snapshot.

use crate::admission::AdmissionRequest;
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::{
    AdmissionGrantClaims, DeploymentRoute, RevocationSnapshot, RouteSnapshot,
};

#[derive(Clone, Debug)]
pub struct RouteRequest<'a> {
    pub admission: &'a AdmissionRequest,
    pub grant: &'a AdmissionGrantClaims,
    pub snapshot: &'a RouteSnapshot,
    pub revocations: &'a RevocationSnapshot,
    pub now_unix_millis: u64,
}

pub fn select_route(request: RouteRequest<'_>) -> FaultResult<DeploymentRoute> {
    let mut eligible = Vec::new();
    for route in &request.snapshot.claims.routes {
        let deployment = route.deployment_id.to_string();
        if route.lease_expires_unix_millis <= request.now_unix_millis
            || request.revocations.deployment_revoked(&deployment)
            || request
                .revocations
                .bundle_revoked(&route.model_bundle.to_string())
            || request
                .revocations
                .bundle_revoked(&route.engine_bundle.to_string())
            || route.region != request.grant.region
        {
            continue;
        }
        if let Some(hint) = &request.admission.deployment_hint {
            if hint != &deployment {
                continue;
            }
        }
        let deployment_allowed = request.grant.allowed_deployments.contains(&deployment);
        let capabilities_allowed = !request.admission.required_capabilities.is_empty()
            && request.admission.required_capabilities.iter().all(|cap| {
                request.grant.allowed_capabilities.contains(cap) && route.capabilities.contains(cap)
            });
        if !deployment_allowed && !capabilities_allowed {
            continue;
        }
        eligible.push(route);
    }
    if eligible.is_empty() {
        return Err(Fault::new(
            Code::Unavailable,
            "no eligible local route exists for this grant",
        ));
    }
    let total_weight = eligible.iter().try_fold(0_u64, |total, route| {
        total
            .checked_add(u64::from(route.weight))
            .ok_or_else(|| Fault::new(Code::OutOfRange, "route weight sum overflow"))
    })?;
    let mut material = request.admission.request_key.clone();
    material.extend_from_slice(request.snapshot.claims.snapshot_digest.as_bytes());
    let digest = hash_bytes(&material);
    let mut first = [0_u8; 8];
    first.copy_from_slice(&digest.as_bytes()[..8]);
    let mut slot = u64::from_be_bytes(first) % total_weight;
    for route in eligible {
        let weight = u64::from(route.weight);
        if slot < weight {
            return Ok(route.clone());
        }
        slot -= weight;
    }
    Err(Fault::internal(
        "weighted route selection exhausted unexpectedly",
    ))
}
