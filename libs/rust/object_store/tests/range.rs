// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteRange;
use mindclade_bytes_io::ByteSize;
use mindclade_object_store::range::RangePolicy;
#[test]
fn range_policy_is_bounded() {
    let p = RangePolicy {
        maximum_read: ByteSize::new(10),
    };
    assert!(p.validate(ByteRange::new(0, 10).unwrap()).is_ok());
    assert!(p.validate(ByteRange::new(0, 11).unwrap()).is_err());
}
