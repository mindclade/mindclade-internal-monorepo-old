#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bio_formats_fuzz::derive_limits;

fuzz_target!(|data: &[u8]| {
    let (limits, body) = derive_limits(data);
    let _ = mindclade_bio_formats::sdf::parse(body, limits);
});
