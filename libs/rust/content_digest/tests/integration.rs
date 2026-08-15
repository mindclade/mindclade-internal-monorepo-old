// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::{Digest, VerifyingReader, hash_bytes};
use std::io::{Cursor, Read};

#[test]
fn sha256_known_vector_and_parse_round_trip() {
    let digest = hash_bytes(b"abc");
    assert_eq!(
        digest.to_string(),
        "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
    assert_eq!(digest.to_string().parse::<Digest>(), Ok(digest));
}

#[test]
fn verifying_reader_reports_corruption() {
    let expected = hash_bytes(b"expected");
    let mut reader = VerifyingReader::new(Cursor::new(b"wrong"), expected);
    let mut output = Vec::new();
    assert!(reader.read_to_end(&mut output).is_err());
}
