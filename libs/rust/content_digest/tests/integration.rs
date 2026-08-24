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

// The source-text tracker in tests/integration/cross_language/test_digest_vectors.py can only
// grep this crate for a match arm. These assert the behaviour it stands in for, so the guarantee
// survives a refactor that moves the decoder without reintroducing the divergence.
#[test]
fn uppercase_hexadecimal_is_refused_as_non_canonical() {
    let lowercase = "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";
    let uppercase = lowercase.to_uppercase().replace("SHA256:", "sha256:");

    let error = uppercase
        .parse::<Digest>()
        .expect_err("uppercase hexadecimal must not parse");
    // Uppercase IS hexadecimal, so the message must not claim otherwise. libs/go/identifiers
    // rejects the same input with "hexadecimal value must be lowercase".
    assert_eq!(
        error.to_string(),
        "digest hexadecimal value must be lowercase"
    );

    assert!(
        lowercase.parse::<Digest>().is_ok(),
        "canonical form still parses"
    );
}

#[test]
fn genuinely_non_hexadecimal_input_keeps_its_own_message() {
    let value = "sha256:zz7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";
    let error = value.parse::<Digest>().expect_err("'z' is not hexadecimal");
    assert_eq!(
        error.to_string(),
        "digest contains a non-hexadecimal character"
    );
}
