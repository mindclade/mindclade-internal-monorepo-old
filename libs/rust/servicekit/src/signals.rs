// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Operating-system termination signals, and the bridge to cooperative shutdown.
//!
//! Process policy still belongs to the embedder: nothing here runs unless a
//! composition root asks for it, and `SignalHandle` remains usable with a
//! shutdown source that is not a signal at all.

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
    #[must_use]
    pub fn request_shutdown(&self) -> bool {
        self.shutdown.cancel()
    }
    #[must_use]
    pub fn token(&self) -> ShutdownToken {
        self.shutdown.clone()
    }
    /// Cancel this handle's token when the operating system asks the process to
    /// terminate.
    ///
    /// The returned task owns the registration and lives until a signal
    /// arrives, so dropping the handle does not disarm it. Abort the task to
    /// stop listening.
    #[must_use]
    pub fn install(&self) -> tokio::task::JoinHandle<()> {
        let shutdown = self.shutdown.clone();
        tokio::spawn(async move {
            termination_requested().await;
            let _ = shutdown.cancel();
        })
    }
}

/// Resolves when the operating system asks this process to terminate.
///
/// SIGTERM is the signal that matters. Kubernetes, Cloud Run, and `docker stop`
/// all send it and none of them send SIGINT, so a process that waits only on
/// `ctrl_c` never drains: it sits idle through the whole termination grace
/// period and is then killed with SIGKILL, mid-request. SIGINT stays handled so
/// interactive runs still stop cleanly.
#[cfg(unix)]
pub async fn termination_requested() {
    use tokio::signal::unix::{SignalKind, signal};

    let Ok(mut terminate) = signal(SignalKind::terminate()) else {
        return interrupt_requested().await;
    };
    tokio::select! {
        _ = terminate.recv() => {}
        () = interrupt_requested() => {}
    }
}

/// Resolves when the operating system asks this process to terminate.
#[cfg(not(unix))]
pub async fn termination_requested() {
    interrupt_requested().await;
}

/// SIGINT, and never resolving if the handler cannot be registered.
///
/// A failed registration must not resolve: callers read a resolve as "shutdown
/// requested", so reporting one here would stop the process during startup
/// rather than report the signal it could not hear.
async fn interrupt_requested() {
    if tokio::signal::ctrl_c().await.is_err() {
        std::future::pending::<()>().await;
    }
}
