// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_worker_protocol::{DetachedSignature, Ed25519KeySet, SignatureVerifier};

fn decode_hex<const N: usize>(value: &str) -> [u8; N] {
    fn nibble(value: u8) -> u8 {
        match value {
            b'0'..=b'9' => value - b'0',
            b'a'..=b'f' => value - b'a' + 10,
            _ => panic!("invalid test vector"),
        }
    }
    let bytes = value.as_bytes();
    assert_eq!(bytes.len(), N * 2);
    let mut output = [0_u8; N];
    for index in 0..N {
        output[index] = (nibble(bytes[index * 2]) << 4) | nibble(bytes[index * 2 + 1]);
    }
    output
}

#[test]
fn verifies_go_stdlib_ed25519_ticket_signature() {
    let payload = include_bytes!(
        "../../../../tests/integration/cross_language/fixtures/execution_ticket_claims_v1.bin"
    );
    let public_key =
        decode_hex::<32>("79b5562e8fe654f94078b112e8a98ba7901f853ae695bed7e0e3910bad049664");
    let signature = decode_hex::<64>(concat!(
        "934f9889a58bc04782c207fa581f046c6cfac4c8beca84c17bbcfa210e4b8172",
        "64542d6578379d92ca67180d25843a5eb2131e47a7d5cf58b25ac0a447ec6a09",
    ));
    let verifier = Ed25519KeySet::new([("runtime/cross-language-v1", public_key)]).expect("keyset");
    verifier
        .verify(
            payload,
            &DetachedSignature {
                algorithm: "ed25519".into(),
                key_id: "runtime/cross-language-v1".into(),
                value: signature.to_vec(),
            },
        )
        .expect("verify Go signature");
}
