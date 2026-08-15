#![forbid(unsafe_code)]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::hash_bytes;
use mindclade_runtime_core::{Budget, ResourceVector};
use std::hint::black_box;
use std::time::Instant;

fn main() {
    let bytes = vec![0x5a_u8; 32 * 1024 * 1024];
    let start = Instant::now();
    for _ in 0..4 {
        black_box(hash_bytes(black_box(&bytes)));
    }
    let seconds = start.elapsed().as_secs_f64();
    let verify_mib_per_s = if seconds > 0.0 {
        128.0 / seconds
    } else {
        f64::INFINITY
    };

    let budget = Budget::root("probe", ResourceVector::default());
    let start = Instant::now();
    for _ in 0..20_000 {
        let reservation = budget
            .reserve(ResourceVector::default())
            .expect("zero reservation");
        black_box(reservation);
    }
    let reserve_us = start.elapsed().as_secs_f64() * 1_000_000.0 / 20_000.0;

    println!(
        "{{\"artifact_verify_mib_per_s\":{verify_mib_per_s:.6},\"runtime_host_invocation_overhead_us\":{reserve_us:.6}}}"
    );
}
