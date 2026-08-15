// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Root-confined atomic filesystem publication and verified reads.
#![forbid(unsafe_code)]

mod path;
mod store;
pub use path::{RelativePath, RelativePathError};
pub use store::{AtomicFileStore, PublishedFile};
