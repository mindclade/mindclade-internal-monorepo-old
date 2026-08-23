#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bio_formats_fuzz::derive_limits;
use mindclade_bounded_parse::ParseMode;

fuzz_target!(|data: &[u8]| {
    let (limits, body) = derive_limits(data);
    // A3M preserves case, so it exercises a different `push_sequence` branch
    // than FASTA over the same bytes.
    let _ = mindclade_bio_formats::parse_a3m(body, limits, ParseMode::Strict);
    let _ = mindclade_bio_formats::parse_a3m(body, limits, ParseMode::Recovery);
});
