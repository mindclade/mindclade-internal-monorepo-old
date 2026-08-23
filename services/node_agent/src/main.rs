// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Node-agent process entry point.
//!
//! The prior entry point printed one line to stderr and returned, which exits
//! 0. To an orchestrator that is a process that started, did its job, and
//! finished successfully — indistinguishable from a clean run, and therefore
//! not a seam at all but a silent failure. Exit 78 (`EX_CONFIG`) is the code
//! `runtime_gateway` and `runtime_host` already use for a composition that
//! could not be satisfied.
#![forbid(unsafe_code)]

use mindclade_node_agent::bootstrap::{self, BootstrapConfig};

#[tokio::main]
async fn main() {
    let outcome = match BootstrapConfig::from_env() {
        Ok(config) => bootstrap::run(config).await,
        Err(error) => Err(error),
    };
    if let Err(error) = outcome {
        eprintln!("mindclade-node-agent failed: {}", error.message());
        std::process::exit(78);
    }
}
