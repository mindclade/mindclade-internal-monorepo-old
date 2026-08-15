// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_tokenizer_runtime::{AlphabetTokenizer, ByteTokenizer, Tokenizer};

#[test]
fn tokenizers_are_deterministic_and_bounded() {
    let byte = ByteTokenizer;
    let encoded = byte.encode(b"abc", 3);
    assert!(encoded.is_ok());
    if let Ok(encoded) = encoded {
        assert_eq!(byte.decode(&encoded.ids, 3).ok(), Some(b"abc".to_vec()));
    }
    let protein = AlphabetTokenizer::protein();
    assert!(protein.is_ok());
    if let Ok(protein) = protein {
        let first = protein.encode(b"ACDX", 4);
        let second = protein.encode(b"ACDX", 4);
        assert_eq!(first.ok(), second.ok());
        assert!(protein.encode(b"ACDX", 3).is_err());
    }
}
