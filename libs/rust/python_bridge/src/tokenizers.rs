// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::FaultResult;
use mindclade_tokenizer_runtime::{Encoding, Tokenizer};

/// Encode text with an explicit token ceiling.
///
/// `maximum_tokens` is threaded through rather than defaulted here: `Tokenizer::encode` treats
/// it as a hard bound, and a wrapper that picks its own would silently truncate at a limit the
/// caller never chose. The trait takes bytes, so the &str is converted at this boundary.
pub fn encode(
    tokenizer: &dyn Tokenizer,
    text: &str,
    maximum_tokens: usize,
) -> FaultResult<Encoding> {
    tokenizer.encode(text.as_bytes(), maximum_tokens)
}
