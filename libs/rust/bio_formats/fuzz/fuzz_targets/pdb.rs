#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bio_formats_fuzz::derive_limits;

fuzz_target!(|data: &[u8]| {
    // The record-name slice is taken at `line[..min(6)]` behind an all-ASCII
    // check; this is the target that would catch that pairing coming apart.
    let (limits, body) = derive_limits(data);
    let _ = mindclade_bio_formats::pdb::parse(body, limits);
});
