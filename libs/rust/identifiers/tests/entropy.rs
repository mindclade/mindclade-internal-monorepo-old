// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! `ResourceId::generate` must not be a pure function of public inputs.
//!
//! This lives in its own test binary on purpose. The predictor below reproduces
//! the exact construction `generate` used before this file existed — a public
//! bijective mix over `(nanoseconds, process counter, pid)` — and that
//! reproduction is only exact for the *first* mint in a process, because the
//! counter is a process global that starts at 1. A second `#[test]` in this file
//! would race for that first mint.
//!
//! The defect this pins is not cosmetic. `libs/rust/atomic_fs` names its
//! partial-write files `.<id body>.partial` in the caller's directory, so a
//! predictable body is a predictable path in a shared directory, and
//! `libs/rust/telemetry_spool`, `libs/rust/checkpoint_io` and
//! `libs/rust/telemetry` mint durable identity from the same call. Go
//! (`crypto/rand`, `libs/go/identifiers/generator.go`) and Python
//! (`secrets.token_bytes`, `libs/python/identifiers/resource.py`) both draw the
//! random field from a CSPRNG; Rust was the outlier.

use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::ManualClock;
use std::time::{Instant, SystemTime};

/// The superseded construction, kept verbatim so the assertion below is a
/// statement about *this* algorithm rather than about randomness in general.
fn predicted_body(nanos: u128, counter: u64, pid: u32, millis: u64) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    fn mix128(mut value: u128) -> u128 {
        value ^= value >> 30;
        value = value.wrapping_mul(0xbf58_476d_1ce4_e5b9_bf58_476d_1ce4_e5b9);
        value ^= value >> 27;
        value = value.wrapping_mul(0x94d0_49bb_1331_11eb_94d0_49bb_1331_11eb);
        value ^ (value >> 31)
    }
    let seed = nanos ^ u128::from(counter).rotate_left(37) ^ u128::from(pid).rotate_left(83);
    let mut uuid = mix128(seed).to_be_bytes();
    let millis_bytes = millis.to_be_bytes();
    uuid[0..6].copy_from_slice(&millis_bytes[2..8]);
    uuid[6] = (uuid[6] & 0x0f) | 0x70;
    uuid[8] = (uuid[8] & 0x3f) | 0x80;
    let mut output = String::with_capacity(32);
    for byte in uuid {
        output.push(char::from(HEX[usize::from(byte >> 4)]));
        output.push(char::from(HEX[usize::from(byte & 0x0f)]));
    }
    output
}

#[test]
fn generated_ids_are_not_a_pure_function_of_clock_pid_and_counter() {
    // UNIX_EPOCH makes the nanosecond and millisecond inputs zero, and this is
    // the process's first mint, so the counter is its initial 1. Everything the
    // old seed consumed is therefore known to this test — which is precisely the
    // property an attacker holding one ID and a clock also had.
    let clock = ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now());
    let minted = ResourceId::generate("run", &clock).expect("mint");
    let predicted = predicted_body(0, 1, std::process::id(), 0);
    assert_ne!(
        minted.body(),
        predicted,
        "resource ID body is derivable from the clock, pid and process counter",
    );
}
