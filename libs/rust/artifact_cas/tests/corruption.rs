// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_artifact_cas::blob::blob_path;
use mindclade_content_digest::hash_bytes;
#[test]
fn digest_path_is_content_addressed() {
    let a = blob_path(hash_bytes(b"a")).unwrap();
    let b = blob_path(hash_bytes(b"b")).unwrap();
    assert_ne!(a, b);
    assert!(a.as_str().starts_with("cas/blobs/sha256/"));
}
