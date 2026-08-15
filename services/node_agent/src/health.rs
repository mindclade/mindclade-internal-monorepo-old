// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Readiness, drain state, and active-stage accounting for node execution.

use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NodeHealthSnapshot {
    pub accepting: bool,
    pub active_stages: u32,
    pub accounting_healthy: bool,
}

#[derive(Debug)]
pub struct NodeHealth {
    accepting: AtomicBool,
    active: AtomicU32,
    accounting_healthy: AtomicBool,
}

impl NodeHealth {
    #[must_use]
    pub fn new() -> Self {
        Self {
            accepting: AtomicBool::new(true),
            active: AtomicU32::new(0),
            accounting_healthy: AtomicBool::new(true),
        }
    }
    /// Acquire an active-stage slot unless drain has begun. The second
    /// accepting check closes the race where drain starts during admission.
    pub fn begin(&self) -> bool {
        if !self.accepting.load(Ordering::Acquire)
            || !self.accounting_healthy.load(Ordering::Acquire)
        {
            return false;
        }
        loop {
            let active = self.active.load(Ordering::Acquire);
            let Some(next) = active.checked_add(1) else {
                self.mark_accounting_corrupt();
                return false;
            };
            match self.active.compare_exchange_weak(
                active,
                next,
                Ordering::AcqRel,
                Ordering::Acquire,
            ) {
                Ok(_) => {
                    if self.accepting.load(Ordering::Acquire)
                        && self.accounting_healthy.load(Ordering::Acquire)
                    {
                        return true;
                    }
                    let _ = self.end();
                    return false;
                }
                Err(_) => continue,
            }
        }
    }
    /// Release one active-stage slot without allowing wraparound on underflow.
    pub fn end(&self) -> bool {
        loop {
            let active = self.active.load(Ordering::Acquire);
            let Some(next) = active.checked_sub(1) else {
                self.mark_accounting_corrupt();
                return false;
            };
            match self.active.compare_exchange_weak(
                active,
                next,
                Ordering::AcqRel,
                Ordering::Acquire,
            ) {
                Ok(_) => return true,
                Err(_) => continue,
            }
        }
    }
    pub fn drain(&self) {
        self.accepting.store(false, Ordering::Release);
    }
    #[must_use]
    pub fn snapshot(&self) -> NodeHealthSnapshot {
        NodeHealthSnapshot {
            accepting: self.accepting.load(Ordering::Acquire),
            active_stages: self.active.load(Ordering::Acquire),
            accounting_healthy: self.accounting_healthy.load(Ordering::Acquire),
        }
    }
    fn mark_accounting_corrupt(&self) {
        self.accounting_healthy.store(false, Ordering::Release);
        self.accepting.store(false, Ordering::Release);
    }
}

impl Default for NodeHealth {
    fn default() -> Self {
        Self::new()
    }
}
