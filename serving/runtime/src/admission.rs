// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Local bounded admission accounting.
//!
//! Global quotas remain a Go control-plane concern. This ledger only enforces
//! the already-authorized grant against concrete node/gateway concurrency and
//! request/input/output budgets.

use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::AdmissionGrantClaims;
use std::collections::{BTreeMap, BTreeSet};
use std::sync::{Arc, Mutex, Weak};

/// Ceiling on the client-supplied capability set, mirroring the 256-capability
/// bound `worker_protocol` already enforces on grant and route capability sets.
/// Only the per-entry length was checked here, so the set itself was unbounded
/// while `select_route` walks it once per eligible route - an untrusted request
/// could therefore buy arbitrary work and residency from a fixed-size grant.
const MAXIMUM_REQUIRED_CAPABILITIES: usize = 256;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdmissionRequest {
    pub request_key: Vec<u8>,
    pub deployment_hint: Option<String>,
    pub required_capabilities: BTreeSet<String>,
    pub input_units: u64,
    pub output_units: u64,
}

impl AdmissionRequest {
    pub fn validate(&self, maximum_key_bytes: usize) -> FaultResult<()> {
        if self.request_key.is_empty() || self.request_key.len() > maximum_key_bytes {
            return Err(Fault::invalid_argument(
                "request key is missing or exceeds its limit",
            ));
        }
        if self.required_capabilities.len() > MAXIMUM_REQUIRED_CAPABILITIES {
            return Err(Fault::invalid_argument(
                "request capability count exceeds its limit",
            ));
        }
        if self
            .required_capabilities
            .iter()
            .any(|v| v.is_empty() || v.len() > 128)
        {
            return Err(Fault::invalid_argument("request capability is invalid"));
        }
        if self
            .deployment_hint
            .as_ref()
            .is_some_and(|v| v.is_empty() || v.len() > 256)
        {
            return Err(Fault::invalid_argument("deployment hint is invalid"));
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
struct GrantUsage {
    active: u32,
    accepted_requests: u64,
    accepted_input_units: u64,
    accepted_output_units: u64,
}

#[derive(Debug)]
struct LedgerState {
    accepting: bool,
    active: u32,
    grants: BTreeMap<String, GrantUsage>,
}

#[derive(Debug)]
struct LedgerInner {
    maximum_active: u32,
    maximum_active_per_grant: u32,
    state: Mutex<LedgerState>,
}

#[derive(Clone, Debug)]
pub struct AdmissionLedger(Arc<LedgerInner>);

impl AdmissionLedger {
    pub fn new(maximum_active: u32, maximum_active_per_grant: u32) -> FaultResult<Self> {
        if maximum_active == 0
            || maximum_active_per_grant == 0
            || maximum_active_per_grant > maximum_active
        {
            return Err(Fault::invalid_argument(
                "admission concurrency limits are invalid",
            ));
        }
        Ok(Self(Arc::new(LedgerInner {
            maximum_active,
            maximum_active_per_grant,
            state: Mutex::new(LedgerState {
                accepting: true,
                active: 0,
                grants: BTreeMap::new(),
            }),
        })))
    }
    pub fn reserve(
        &self,
        grant: &AdmissionGrantClaims,
        request: &AdmissionRequest,
    ) -> FaultResult<AdmissionPermit> {
        let grant_id = grant.grant_id.to_string();
        let mut state = self
            .0
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if !state.accepting {
            return Err(Fault::new(Code::Unavailable, "local admission is draining"));
        }
        let current = state.grants.get(&grant_id).copied().unwrap_or_default();
        let grant_concurrency = grant
            .maximum_concurrency
            .min(self.0.maximum_active_per_grant);
        if state.active >= self.0.maximum_active || current.active >= grant_concurrency {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "local admission concurrency is exhausted",
            ));
        }
        if current.accepted_requests >= grant.maximum_requests {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "admission grant request budget is exhausted",
            ));
        }
        let next_input = current
            .accepted_input_units
            .checked_add(request.input_units)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "admission input accounting overflow"))?;
        let next_output = current
            .accepted_output_units
            .checked_add(request.output_units)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "admission output accounting overflow"))?;
        if grant.maximum_input_units != 0 && next_input > grant.maximum_input_units {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "admission grant input budget is exhausted",
            ));
        }
        if grant.maximum_output_units != 0 && next_output > grant.maximum_output_units {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "admission grant output budget is exhausted",
            ));
        }
        state.active += 1;
        state.grants.insert(
            grant_id.clone(),
            GrantUsage {
                active: current.active + 1,
                accepted_requests: current.accepted_requests + 1,
                accepted_input_units: next_input,
                accepted_output_units: next_output,
            },
        );
        Ok(AdmissionPermit {
            ledger: Arc::downgrade(&self.0),
            grant_id,
            released: false,
        })
    }
    #[must_use]
    pub fn active(&self) -> u32 {
        self.0
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .active
    }

    /// Atomically closes admission before lifecycle readiness is withdrawn.
    pub fn begin_drain(&self) {
        self.0
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .accepting = false;
    }

    /// Reopens local admission after policy and downstream readiness recover.
    pub fn resume(&self) {
        self.0
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .accepting = true;
    }
}

#[derive(Debug)]
pub struct AdmissionPermit {
    ledger: Weak<LedgerInner>,
    grant_id: String,
    released: bool,
}

impl AdmissionPermit {
    pub fn release(mut self) {
        self.release_inner();
    }
    fn release_inner(&mut self) {
        if self.released {
            return;
        }
        self.released = true;
        let Some(ledger) = self.ledger.upgrade() else {
            return;
        };
        let mut state = ledger
            .state
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if state.active > 0 {
            state.active -= 1;
        }
        if let Some(usage) = state.grants.get_mut(&self.grant_id)
            && usage.active > 0
        {
            usage.active -= 1;
        }
    }
}

impl Drop for AdmissionPermit {
    fn drop(&mut self) {
        self.release_inner();
    }
}
