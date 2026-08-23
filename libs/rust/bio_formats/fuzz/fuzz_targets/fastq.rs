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
    // FASTQ reads four lines per record, so truncation mid-record is its own
    // failure mode and the sequence/quality length agreement is a real invariant.
    let _ = mindclade_bio_formats::parse_fastq(body, limits, ParseMode::Strict);
    let _ = mindclade_bio_formats::parse_fastq(body, limits, ParseMode::Recovery);
});
