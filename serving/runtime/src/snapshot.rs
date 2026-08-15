// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Locally verified immutable policy snapshots.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::{
    AdmissionGrant, RevocationSnapshot, RouteSnapshot, SignatureVerifier,
};
use std::sync::{Arc, RwLock};

#[derive(Clone, Debug)]
pub struct PolicySnapshot {
    pub route: RouteSnapshot,
    pub revocations: RevocationSnapshot,
    pub minimum_policy_epoch: u64,
    pub minimum_route_version: u64,
    pub minimum_revocation_epoch: u64,
}

struct PolicyInner {
    route: Option<RouteSnapshot>,
    revocations: RevocationSnapshot,
    minimum_policy_epoch: u64,
    minimum_route_version: u64,
    minimum_revocation_epoch: u64,
}

pub struct PolicyCache {
    verifier: Arc<dyn SignatureVerifier>,
    inner: RwLock<PolicyInner>,
}

impl core::fmt::Debug for PolicyCache {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        let inner = self
            .inner
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        formatter
            .debug_struct("PolicyCache")
            .field("has_route", &inner.route.is_some())
            .field("minimum_policy_epoch", &inner.minimum_policy_epoch)
            .field("minimum_route_version", &inner.minimum_route_version)
            .field("minimum_revocation_epoch", &inner.minimum_revocation_epoch)
            .finish()
    }
}

impl PolicyCache {
    pub fn new(
        verifier: Arc<dyn SignatureVerifier>,
        bootstrap_revocations: RevocationSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<Self> {
        bootstrap_revocations.validate(
            now_unix_millis,
            bootstrap_revocations.claims.epoch,
            verifier.as_ref(),
        )?;
        let epoch = bootstrap_revocations.claims.epoch;
        Ok(Self {
            verifier,
            inner: RwLock::new(PolicyInner {
                route: None,
                revocations: bootstrap_revocations,
                minimum_policy_epoch: 1,
                minimum_route_version: 1,
                minimum_revocation_epoch: epoch,
            }),
        })
    }
    pub fn install_revocations(
        &self,
        snapshot: RevocationSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        let mut inner = self
            .inner
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        snapshot.validate(
            now_unix_millis,
            inner.minimum_revocation_epoch,
            self.verifier.as_ref(),
        )?;
        if snapshot.claims.epoch < inner.revocations.claims.epoch {
            return Err(Fault::new(
                Code::Conflict,
                "revocation snapshot would move backwards",
            ));
        }
        inner.minimum_revocation_epoch = snapshot.claims.epoch;
        inner.revocations = snapshot;
        Ok(())
    }
    pub fn install_route(&self, snapshot: RouteSnapshot, now_unix_millis: u64) -> FaultResult<()> {
        let mut inner = self
            .inner
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        snapshot.validate(
            now_unix_millis,
            inner.minimum_policy_epoch,
            inner.minimum_route_version,
            &inner.revocations,
            self.verifier.as_ref(),
        )?;
        if let Some(current) = &inner.route
            && snapshot.claims.version <= current.claims.version
        {
            return Err(Fault::new(
                Code::Conflict,
                "route snapshot version is not monotonic",
            ));
        }
        inner.minimum_policy_epoch = inner.minimum_policy_epoch.max(snapshot.claims.policy_epoch);
        inner.minimum_route_version = snapshot.claims.version;
        inner.route = Some(snapshot);
        Ok(())
    }
    pub fn raise_policy_floor(&self, policy_epoch: u64, route_version: u64) -> FaultResult<()> {
        if policy_epoch == 0 || route_version == 0 {
            return Err(Fault::invalid_argument("policy floors must be non-zero"));
        }
        let mut inner = self
            .inner
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        inner.minimum_policy_epoch = inner.minimum_policy_epoch.max(policy_epoch);
        inner.minimum_route_version = inner.minimum_route_version.max(route_version);
        Ok(())
    }
    pub fn validate_grant(&self, grant: &AdmissionGrant, now_unix_millis: u64) -> FaultResult<()> {
        let inner = self
            .inner
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        grant.validate(
            now_unix_millis,
            inner.minimum_policy_epoch,
            &inner.revocations,
            self.verifier.as_ref(),
        )
    }
    pub fn snapshot(&self, now_unix_millis: u64) -> FaultResult<PolicySnapshot> {
        let inner = self
            .inner
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let route = inner
            .route
            .clone()
            .ok_or_else(|| Fault::new(Code::Unavailable, "no route snapshot is installed"))?;
        route.validate(
            now_unix_millis,
            inner.minimum_policy_epoch,
            inner.minimum_route_version,
            &inner.revocations,
            self.verifier.as_ref(),
        )?;
        inner.revocations.validate(
            now_unix_millis,
            inner.minimum_revocation_epoch,
            self.verifier.as_ref(),
        )?;
        Ok(PolicySnapshot {
            route,
            revocations: inner.revocations.clone(),
            minimum_policy_epoch: inner.minimum_policy_epoch,
            minimum_route_version: inner.minimum_route_version,
            minimum_revocation_epoch: inner.minimum_revocation_epoch,
        })
    }
}
