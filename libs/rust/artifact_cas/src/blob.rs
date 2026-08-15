// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic content-addressed blob paths.

use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};
use mindclade_object_store::ObjectPath;

/// Map a SHA-256 digest into a two-character fan-out namespace.
pub fn blob_path(digest: Digest) -> FaultResult<ObjectPath> {
    let hex = digest.to_hex();
    let prefix = hex
        .get(..2)
        .ok_or_else(|| Fault::internal("digest hex encoding is too short"))?;
    ObjectPath::new(format!("cas/blobs/sha256/{prefix}/{hex}"))
        .map_err(|error| Fault::internal("failed to construct CAS blob path").with_source(error))
}
