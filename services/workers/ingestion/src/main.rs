// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

mod config;
mod executor;
mod lifecycle;

fn main() {
    eprintln!(
        "mindclade-ingestion-worker: provider/source engine not configured; \
         ticketed worker adapter is implemented and startup fails closed"
    );
    std::process::exit(78);
}
