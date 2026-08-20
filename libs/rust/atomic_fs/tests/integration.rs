// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_atomic_fs::{AtomicFileStore, RelativePath};
use mindclade_content_digest::hash_bytes;
use mindclade_faults::Code;
use std::fs;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

static TEMPORARY_ROOT_SEQUENCE: AtomicU64 = AtomicU64::new(0);

fn temporary_root() -> std::path::PathBuf {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos();
    std::env::temp_dir().join(format!(
        "mindclade-atomic-fs-{}-{nanos}-{}",
        std::process::id(),
        TEMPORARY_ROOT_SEQUENCE.fetch_add(1, Ordering::Relaxed),
    ))
}

#[test]
fn publishes_and_verifies_content() {
    let root = temporary_root();
    let store = AtomicFileStore::new(&root);
    assert!(store.is_ok());
    if let Ok(store) = store {
        let path = RelativePath::new("objects/value.bin");
        assert!(path.is_ok());
        if let Ok(path) = path {
            assert!(store.publish(path.clone(), b"payload", false).is_ok());
            assert_eq!(
                store
                    .read_verified(&path, hash_bytes(b"payload"), 1024)
                    .ok(),
                Some(b"payload".to_vec())
            );
        }
    }
    let _ = fs::remove_dir_all(root);
}

#[test]
fn rejects_path_traversal() {
    assert!(RelativePath::new("../secret").is_err());
    assert!(RelativePath::new("/absolute").is_err());
}

#[test]
fn rejects_oversized_and_modified_content() {
    let root = temporary_root();
    let store = AtomicFileStore::new(&root).expect("temporary store should be created");
    let path = RelativePath::new("objects/value.bin").expect("relative path should be valid");
    store
        .publish(path.clone(), b"payload", false)
        .expect("fixture should publish");

    let oversized = store
        .read_verified(&path, hash_bytes(b"payload"), 6)
        .expect_err("read limit must be enforced");
    assert_eq!(oversized.code(), Code::ResourceExhausted);

    fs::write(root.join(path.as_path()), b"tampered").expect("fixture should be mutable");
    let modified = store
        .read_verified(&path, hash_bytes(b"payload"), 1024)
        .expect_err("digest mismatch must be rejected");
    assert_eq!(modified.code(), Code::DataLoss);

    let _ = fs::remove_dir_all(root);
}
