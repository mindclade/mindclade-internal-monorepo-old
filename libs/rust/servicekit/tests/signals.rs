// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Termination-signal contract.
//!
//! Lives in `tests/` rather than beside the code on purpose: this repository's
//! `rust_test` targets never set `crate = ":..."`, so inline `#[cfg(test)]`
//! modules run under `cargo test` but not under Bazel. A regression this cheap
//! to reintroduce needs to run everywhere.

use mindclade_servicekit::{ShutdownToken, SignalHandle, signals};
use std::{process::Command, time::Duration};

/// Raise SIGTERM at this process once a handler is guaranteed to be installed.
#[cfg(unix)]
fn raise_sigterm() {
    let status = Command::new("kill")
        .args(["-TERM", &std::process::id().to_string()])
        .status()
        .expect("kill runs");
    assert!(status.success(), "could not signal the test process");
}

/// The regression this guards: `ctrl_c` alone is SIGINT, and every orchestrator
/// these binaries run under sends SIGTERM instead. A process that hears only
/// SIGINT never drains — it is killed with SIGKILL when the grace period ends.
#[cfg(unix)]
#[tokio::test]
async fn sigterm_resolves_termination_requested() {
    use tokio::signal::unix::{SignalKind, signal};

    // Install BEFORE raising. Until some stream exists the default disposition
    // is still in place and the raise would terminate the test binary.
    let _installed = signal(SignalKind::terminate()).expect("SIGTERM handler installs");

    let waiter = tokio::spawn(signals::termination_requested());
    tokio::task::yield_now().await;
    raise_sigterm();

    tokio::time::timeout(Duration::from_secs(5), waiter)
        .await
        .expect("termination_requested must resolve on SIGTERM")
        .expect("waiter task joins");
}

/// `SignalHandle::install` is the bridge the composition roots actually use:
/// the OS signal has to reach the cooperative shutdown token.
#[cfg(unix)]
#[tokio::test]
async fn install_cancels_the_shutdown_token_on_sigterm() {
    use tokio::signal::unix::{SignalKind, signal};

    let _installed = signal(SignalKind::terminate()).expect("SIGTERM handler installs");

    let token = ShutdownToken::new();
    let handle = SignalHandle::new(token.clone());
    let task = handle.install();
    tokio::task::yield_now().await;

    assert!(!token.is_cancelled(), "token must start uncancelled");
    raise_sigterm();

    tokio::time::timeout(Duration::from_secs(5), task)
        .await
        .expect("install task must observe SIGTERM")
        .expect("install task joins");
    assert!(
        token.is_cancelled(),
        "SIGTERM must cancel the shutdown token"
    );
}

/// Cancelling without a signal must still work: the handle is a shutdown
/// bridge, not a signal-only path.
#[test]
fn request_shutdown_cancels_without_any_signal() {
    let token = ShutdownToken::new();
    let handle = SignalHandle::new(token.clone());
    assert!(handle.request_shutdown(), "first cancel reports the change");
    assert!(token.is_cancelled());
    assert!(!handle.request_shutdown(), "cancelling twice is idempotent");
}
