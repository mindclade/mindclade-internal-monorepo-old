// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Fast local health/readiness state.

use std::sync::atomic::{AtomicBool, Ordering};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct GatewayHealthSnapshot {
    pub accepting: bool,
    pub policy_fresh: bool,
    /// The node-local runtime host's control socket was **reachable** at the
    /// last probe round -- not that the host reports itself ready.
    ///
    /// `WorkerControl` carries only `rpc Execute`, so there is no host health
    /// RPC to ask; `host_probe` therefore connects to the socket named by the
    /// installed route snapshot and publishes what it observed. The flag keeps
    /// its original spelling because `docs/runbooks/runtime-gateway-degraded.md`
    /// and `docs/slo/runtime-gateway.md` cite it by name.
    pub runtime_host_ready: bool,
}

impl GatewayHealthSnapshot {
    #[must_use]
    pub const fn live(self) -> bool {
        true
    }
    #[must_use]
    pub const fn ready(self) -> bool {
        self.accepting && self.policy_fresh && self.runtime_host_ready
    }
}

#[derive(Debug)]
pub struct GatewayHealth {
    accepting: AtomicBool,
    policy_fresh: AtomicBool,
    runtime_host_ready: AtomicBool,
}

impl GatewayHealth {
    #[must_use]
    pub fn new() -> Self {
        Self {
            accepting: AtomicBool::new(false),
            policy_fresh: AtomicBool::new(false),
            runtime_host_ready: AtomicBool::new(false),
        }
    }
    pub fn set_accepting(&self, value: bool) {
        self.accepting.store(value, Ordering::Release);
    }
    pub fn set_policy_fresh(&self, value: bool) {
        self.policy_fresh.store(value, Ordering::Release);
    }
    /// Publish the latest runtime-host reachability observation. Owned by
    /// `host_probe::run`; nothing else in the process may assert it, because a
    /// value not derived from an actual connection would be a readiness claim
    /// with no evidence behind it.
    pub fn set_runtime_host_ready(&self, value: bool) {
        self.runtime_host_ready.store(value, Ordering::Release);
    }
    #[must_use]
    pub fn snapshot(&self) -> GatewayHealthSnapshot {
        GatewayHealthSnapshot {
            accepting: self.accepting.load(Ordering::Acquire),
            policy_fresh: self.policy_fresh.load(Ordering::Acquire),
            runtime_host_ready: self.runtime_host_ready.load(Ordering::Acquire),
        }
    }
}

impl Default for GatewayHealth {
    fn default() -> Self {
        Self::new()
    }
}
