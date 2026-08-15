// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Bounded response multiplexing is implemented once in `serving/runtime`.
pub use mindclade_serving_runtime::streaming::{
    ResponseChunk, StreamReceiver, StreamSender, bounded_stream,
};
