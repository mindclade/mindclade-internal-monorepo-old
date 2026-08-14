//! Injectable wall and monotonic clocks.
#![forbid(unsafe_code)]

use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime};

/// Source of wall and monotonic time.
pub trait Clock: Send + Sync {
    /// Current wall-clock time.
    fn system_now(&self) -> SystemTime;
    /// Current monotonic instant.
    fn monotonic_now(&self) -> Instant;
}

/// Production clock backed by the standard library.
#[derive(Clone, Copy, Debug, Default)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn system_now(&self) -> SystemTime {
        SystemTime::now()
    }
    fn monotonic_now(&self) -> Instant {
        Instant::now()
    }
}

#[derive(Debug)]
struct ManualState {
    system: SystemTime,
    monotonic: Instant,
}

/// Deterministic clock for tests and simulations.
#[derive(Clone, Debug)]
pub struct ManualClock {
    state: Arc<Mutex<ManualState>>,
}

impl ManualClock {
    /// Creates a manual clock.
    #[must_use]
    pub fn new(system: SystemTime, monotonic: Instant) -> Self {
        Self {
            state: Arc::new(Mutex::new(ManualState { system, monotonic })),
        }
    }
    /// Advances both time domains by the same duration.
    pub fn advance(&self, duration: Duration) {
        let mut state = self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        state.system = state.system.checked_add(duration).unwrap_or(SystemTime::UNIX_EPOCH);
        state.monotonic = state.monotonic.checked_add(duration).unwrap_or(state.monotonic);
    }
    /// Sets wall time while preserving monotonic time.
    pub fn set_system_time(&self, time: SystemTime) {
        self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner()).system = time;
    }
}

impl Clock for ManualClock {
    fn system_now(&self) -> SystemTime {
        self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner()).system
    }
    fn monotonic_now(&self) -> Instant {
        self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner()).monotonic
    }
}
