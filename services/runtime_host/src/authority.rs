// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Offline signed-authority state for the runtime host.
//!
//! The host independently validates every execution ticket. Policy floors and
//! revocations may only advance. This prevents stale or duplicated workers from
//! committing after a newer Go control-plane lease/policy epoch takes effect.

use crate::{ExecutionSession, HostCore};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_serving_runtime::host::HostInvocation;
use mindclade_worker_protocol::{
    Ed25519KeySet, Ed25519VerificationKey, RevocationSnapshot, SignatureVerifier,
};
use std::sync::{Arc, RwLock};

#[derive(Clone, Debug)]
struct AuthorityState {
    revocations: RevocationSnapshot,
    minimum_policy_epoch: u64,
    minimum_route_version: u64,
    minimum_revocation_epoch: u64,
}

pub struct HostAuthority {
    verifier: Arc<dyn SignatureVerifier>,
    state: RwLock<AuthorityState>,
}

impl core::fmt::Debug for HostAuthority {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        let state = self
            .state
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        formatter
            .debug_struct("HostAuthority")
            .field("minimum_policy_epoch", &state.minimum_policy_epoch)
            .field("minimum_route_version", &state.minimum_route_version)
            .field("minimum_revocation_epoch", &state.minimum_revocation_epoch)
            .finish_non_exhaustive()
    }
}

impl HostAuthority {
    pub fn with_verifier(
        verifier: Arc<dyn SignatureVerifier>,
        bootstrap_revocations: RevocationSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<Self> {
        bootstrap_revocations.validate(
            now_unix_millis,
            bootstrap_revocations.claims.epoch,
            verifier.as_ref(),
        )?;
        let minimum_revocation_epoch = bootstrap_revocations.claims.epoch;
        Ok(Self {
            verifier,
            state: RwLock::new(AuthorityState {
                revocations: bootstrap_revocations,
                minimum_policy_epoch: 1,
                minimum_route_version: 1,
                minimum_revocation_epoch,
            }),
        })
    }

    pub fn from_ed25519_keys<I>(
        keys: I,
        bootstrap_revocations: RevocationSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<Self>
    where
        I: IntoIterator<Item = Ed25519VerificationKey>,
    {
        let verifier: Arc<dyn SignatureVerifier> = Arc::new(Ed25519KeySet::with_clock(
            Arc::new(mindclade_runtime_core::SystemClock),
            keys,
        )?);
        Self::with_verifier(verifier, bootstrap_revocations, now_unix_millis)
    }
    pub fn install_revocations(
        &self,
        snapshot: RevocationSnapshot,
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        let mut state = self
            .state
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        snapshot.validate(
            now_unix_millis,
            state.minimum_revocation_epoch,
            self.verifier.as_ref(),
        )?;
        if snapshot.claims.epoch < state.revocations.claims.epoch {
            return Err(Fault::new(
                Code::Conflict,
                "runtime host revocation epoch would move backwards",
            ));
        }
        state.minimum_revocation_epoch = snapshot.claims.epoch;
        state.revocations = snapshot;
        Ok(())
    }
    pub fn raise_policy_floor(
        &self,
        policy_epoch: u64,
        route_version: u64,
        revocation_epoch: u64,
    ) -> FaultResult<()> {
        if policy_epoch == 0 || route_version == 0 || revocation_epoch == 0 {
            return Err(Fault::invalid_argument(
                "runtime host policy floors must be non-zero",
            ));
        }
        let mut state = self
            .state
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        state.minimum_policy_epoch = state.minimum_policy_epoch.max(policy_epoch);
        state.minimum_route_version = state.minimum_route_version.max(route_version);
        state.minimum_revocation_epoch = state.minimum_revocation_epoch.max(revocation_epoch);
        Ok(())
    }
    pub fn begin_invocation(
        &self,
        host: &HostCore,
        invocation: HostInvocation,
        now_unix_millis: u64,
    ) -> FaultResult<ExecutionSession> {
        let state = self
            .state
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .clone();
        host.begin_invocation(
            invocation,
            now_unix_millis,
            state.minimum_policy_epoch,
            state.minimum_route_version,
            state.minimum_revocation_epoch,
            &state.revocations,
            self.verifier.as_ref(),
        )
    }
}
