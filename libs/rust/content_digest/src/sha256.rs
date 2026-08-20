// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Incremental SHA-256 implementation backed by `RustCrypto`.

use crate::Digest;
use sha2::Digest as _;

/// Incremental SHA-256 state.
///
/// This wrapper keeps Mindclade's stable digest API independent of the
/// cryptographic provider while using `RustCrypto`'s optimized, widely reviewed
/// compression implementation.
#[derive(Clone, Debug, Default)]
pub struct Sha256(sha2::Sha256);

impl Sha256 {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    pub fn update(&mut self, input: &[u8]) {
        self.0.update(input);
    }

    #[must_use]
    pub fn finalize(self) -> Digest {
        let output = self.0.finalize();
        let mut bytes = [0_u8; 32];
        bytes.copy_from_slice(&output);
        Digest::from_bytes(bytes)
    }
}

#[must_use]
pub fn hash_bytes(bytes: &[u8]) -> Digest {
    let mut state = Sha256::new();
    state.update(bytes);
    state.finalize()
}
