//! Signal adapters remain outside the core so embedders can own process policy.

use crate::ShutdownToken;

#[derive(Clone, Debug)]
pub struct SignalHandle {
    shutdown: ShutdownToken,
}

impl SignalHandle {
    #[must_use]
    pub fn new(shutdown: ShutdownToken) -> Self {
        Self { shutdown }
    }
    /// Idempotently request process shutdown.
    pub fn request_shutdown(&self) -> bool {
        self.shutdown.cancel()
    }
    #[must_use]
    pub fn token(&self) -> ShutdownToken {
        self.shutdown.clone()
    }
}
