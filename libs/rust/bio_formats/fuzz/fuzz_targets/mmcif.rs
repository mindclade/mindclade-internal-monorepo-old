#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bounded_parse::Limits;

fuzz_target!(|data: &[u8]| {
    let _ = mindclade_bio_formats::mmcif::parse(data, Limits::default());
});
