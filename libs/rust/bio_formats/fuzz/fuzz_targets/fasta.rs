#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bounded_parse::{Limits, ParseMode};

fuzz_target!(|data: &[u8]| {
    let _ = mindclade_bio_formats::parse_fasta(data, Limits::default(), ParseMode::Strict);
});
