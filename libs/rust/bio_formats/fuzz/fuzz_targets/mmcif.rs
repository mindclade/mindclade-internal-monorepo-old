#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bio_formats_fuzz::derive_limits;

fuzz_target!(|data: &[u8]| {
    // The semicolon text field accumulates across lines and is the only place
    // one token can outgrow one line, so the token ceiling is worth driving
    // with fuzzer-chosen limits rather than the default 1 MiB.
    let (limits, body) = derive_limits(data);
    let _ = mindclade_bio_formats::mmcif::parse(body, limits);
});
