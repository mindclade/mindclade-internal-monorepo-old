#![no_main]
// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use libfuzzer_sys::fuzz_target;
use mindclade_bytes_io::ByteSize;

fuzz_target!(|data: &[u8]| {
    let _ = mindclade_ipc::Message::decode(data, ByteSize::new(1024 * 1024));
});
