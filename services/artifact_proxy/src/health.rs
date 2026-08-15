// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Artifact proxy health, drain state, and strict transfer admission.

use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProxyHealthSnapshot {
    pub accepting: bool,
    pub active_transfers: u32,
    pub accounting_healthy: bool,
}

#[derive(Debug)]
pub struct ProxyHealth {
    accepting: AtomicBool,
    active: AtomicU32,
    accounting_healthy: AtomicBool,
}

impl ProxyHealth {
    #[must_use]
    pub fn new() -> Self {
        Self {
            accepting: AtomicBool::new(true),
            active: AtomicU32::new(0),
            accounting_healthy: AtomicBool::new(true),
        }
    }
    /// Acquire one transfer permit without ever exceeding `maximum`.
    pub fn try_begin(&self, maximum: u32) -> bool {
        if maximum == 0
            || !self.accepting.load(Ordering::Acquire)
            || !self.accounting_healthy.load(Ordering::Acquire)
        {
            return false;
        }
        loop {
            let active = self.active.load(Ordering::Acquire);
            if active >= maximum {
                return false;
            }
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
    /// Compatibility helper for low-level callers with no explicit cap.
    pub fn begin(&self) -> bool {
        self.try_begin(u32::MAX)
    }
    /// Release one transfer permit. Underflow permanently fails readiness and
    /// admission rather than wrapping the active count to `u32::MAX`.
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
    pub fn snapshot(&self) -> ProxyHealthSnapshot {
        ProxyHealthSnapshot {
            accepting: self.accepting.load(Ordering::Acquire),
            active_transfers: self.active.load(Ordering::Acquire),
            accounting_healthy: self.accounting_healthy.load(Ordering::Acquire),
        }
    }
    fn mark_accounting_corrupt(&self) {
        self.accounting_healthy.store(false, Ordering::Release);
        self.accepting.store(false, Ordering::Release);
    }
}

impl Default for ProxyHealth {
    fn default() -> Self {
        Self::new()
    }
}
