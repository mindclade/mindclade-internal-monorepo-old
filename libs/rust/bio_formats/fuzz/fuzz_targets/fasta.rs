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
    // Both modes: recovery skips invalid bytes and keeps going, so it walks
    // paths that strict mode returns from early.
    let _ = mindclade_bio_formats::parse_fasta(body, limits, ParseMode::Strict);
    let _ = mindclade_bio_formats::parse_fasta(body, limits, ParseMode::Recovery);
});
