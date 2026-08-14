//! Offline runtime authority construction for the gateway.
//!
//! Go owns issuance and policy. The Rust gateway receives bounded verification
//! keys plus signed immutable snapshots and never calls a policy database on
//! the request path.

use crate::PolicyCache;
use mindclade_faults::{Fault, FaultResult};
use mindclade_worker_protocol::{
    Ed25519KeySet, Ed25519VerificationKey, RevocationSnapshot, RouteSnapshot,
};
use std::sync::Arc;

#[derive(Debug)]
pub struct GatewayAuthority {
    policy: Arc<PolicyCache>,
}

impl GatewayAuthority {
    pub fn from_ed25519_keys<I>(
        keys: I,
        bootstrap_revocations: RevocationSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<Self>
    where
        I: IntoIterator<Item = Ed25519VerificationKey>,
    {
        let verifier = Arc::new(Ed25519KeySet::with_clock(
            Arc::new(mindclade_runtime_core::SystemClock),
            keys,
        )?);
        let policy = Arc::new(PolicyCache::new(
            verifier,
            bootstrap_revocations,
            now_unix_millis,
        )?);
        Ok(Self { policy })
    }
    #[must_use]
    pub fn policy(&self) -> Arc<PolicyCache> {
        self.policy.clone()
    }
    pub fn install_route(
        &self,
        snapshot: RouteSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        self.policy.install_route(snapshot, now_unix_millis)
    }
    pub fn install_revocations(
        &self,
        snapshot: RevocationSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        self.policy.install_revocations(snapshot, now_unix_millis)
    }
    pub fn raise_policy_floor(&self, policy_epoch: u64, route_version: u64) -> FaultResult<()> {
        if policy_epoch == 0 || route_version == 0 {
            return Err(Fault::invalid_argument("runtime policy floors must be non-zero"));
        }
        self.policy.raise_policy_floor(policy_epoch, route_version)
    }
}
