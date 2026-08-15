// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteSize;
use mindclade_record_io::RecordReader;
#[test]
fn truncated_frame_fails_closed() {
    let mut r = RecordReader::new(&b"MCRD\0"[..], ByteSize::new(1024));
    assert!(r.read_next().is_err());
}
