// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Artifact-proxy process entry point.
//!
//! The prior entry point printed one line to stderr and returned, which exits
//! 0. The README called that "deliberately a fail-closed composition seam", but
//! a zero exit is the success code: to an orchestrator this was a process that
//! started, did its job, and finished cleanly — indistinguishable from a real
//! run, and therefore a silent failure rather than a seam. Exit 78
//! (`EX_CONFIG`) is the code `runtime_gateway` and `runtime_host` already use
//! for a composition that could not be satisfied.
#![forbid(unsafe_code)]

use mindclade_artifact_proxy::bootstrap::{self, BootstrapConfig};

#[tokio::main]
async fn main() {
    let outcome = match BootstrapConfig::from_env() {
        Ok(config) => bootstrap::run(config).await,
        Err(error) => Err(error),
    };
    if let Err(error) = outcome {
        eprintln!("mindclade-artifact-proxy failed: {}", error.message());
        std::process::exit(78);
    }
}
