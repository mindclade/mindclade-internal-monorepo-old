// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::FaultResult;
use mindclade_tokenizer_runtime::{Encoding, Tokenizer};

pub fn encode(tokenizer: &dyn Tokenizer, text: &str) -> FaultResult<Encoding> {
    tokenizer.encode(text)
}
