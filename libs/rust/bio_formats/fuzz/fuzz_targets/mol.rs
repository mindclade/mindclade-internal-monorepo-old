#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bio_formats_fuzz::derive_limits;

fuzz_target!(|data: &[u8]| {
    // MOL is the one format with a declared counts line, and it byte-indexes
    // `counts[0..3]` / `counts[3..6]` behind a length-and-ASCII guard. Both the
    // arithmetic on the declared counts and that slicing want fuzzing.
    let (limits, body) = derive_limits(data);
    let _ = mindclade_bio_formats::mol::parse(body, limits);
});
