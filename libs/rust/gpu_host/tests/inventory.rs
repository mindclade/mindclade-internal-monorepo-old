// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_gpu_host::providers::nvidia;
#[test]
fn inventory_line_is_bounded() {
    let d = nvidia::from_inventory_line("hopper,85899345920").unwrap();
    assert_eq!(d.vendor, "nvidia");
}
