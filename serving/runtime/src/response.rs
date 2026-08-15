// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Model-neutral response representation.
use mindclade_content_digest::Digest;
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InferenceResponse {
    pub request_id: String,
    pub terminal: bool,
    pub payload: Vec<u8>,
    pub artifact: Option<Digest>,
}
pub use crate::streaming::ResponseChunk;
