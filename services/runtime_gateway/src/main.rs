// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Runtime-gateway process entry point.
#![forbid(unsafe_code)]

use mindclade_runtime_gateway::bootstrap::{self, BootstrapConfig};

#[tokio::main]
async fn main() {
    let outcome = match BootstrapConfig::from_env() {
        Ok(config) => bootstrap::run(config).await,
        Err(error) => Err(error),
    };
    if let Err(error) = outcome {
        eprintln!("mindclade-runtime-gateway failed: {}", error.message());
        std::process::exit(78);
    }
}
