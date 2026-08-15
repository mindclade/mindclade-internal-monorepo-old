// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bounded_parse::Limits;
use mindclade_python_bridge::parsers;
#[test]
fn parser_leaf_is_bounded() {
    let r = parsers::fasta(b">x\nAC\n", Limits::default()).unwrap();
    assert_eq!(r[0].sequence, b"AC");
}
