// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use ed25519_dalek::{Signer, SigningKey};
use mindclade_worker_protocol::{DetachedSignature, Ed25519KeySet, SignatureVerifier};

#[test]
fn ed25519_verifier_accepts_go_compatible_detached_signature_shape() {
    let seed = [7_u8; 32];
    let signing = SigningKey::from_bytes(&seed);
    let verifying = signing.verifying_key();
    let payload = b"MCCE1/test\0";
    let signature = signing.sign(payload);
    let keyset = Ed25519KeySet::new([("runtime/primary", verifying.to_bytes())]).expect("keyset");
    keyset
        .verify(
            payload,
            &DetachedSignature {
                algorithm: "ed25519".into(),
                key_id: "runtime/primary".into(),
                value: signature.to_bytes().to_vec(),
            },
        )
        .expect("verify");
}
