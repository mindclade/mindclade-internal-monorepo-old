// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_object_store::{LocalStore, MemoryStore, ObjectPath, ObjectStore, PutCondition};
use std::sync::{Arc, Barrier};
use std::time::{SystemTime, UNIX_EPOCH};

fn temporary_root() -> std::path::PathBuf {
    std::env::temp_dir().join(format!(
        "mindclade-local-store-{}-{}",
        std::process::id(),
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock should be after epoch")
            .as_nanos()
    ))
}

#[test]
fn memory_store_enforces_conditions_and_ranges() {
    let store = MemoryStore::new();
    let path = ObjectPath::new("checkpoints/run/shard-0");
    assert!(path.is_ok());
    if let Ok(path) = path {
        let first = store.put(&path, b"abcdef", PutCondition::CreateOnly);
        assert!(first.is_ok());
        assert!(
            store
                .put(&path, b"other", PutCondition::CreateOnly)
                .is_err()
        );
        let range = ByteRange::new(2, 3);
        assert!(range.is_ok());
        if let Ok(range) = range {
            assert_eq!(store.get_range(&path, range).ok(), Some(b"cde".to_vec()));
        }
        assert_eq!(
            store.get(&path, ByteSize::new(6)).ok(),
            Some(b"abcdef".to_vec())
        );
    }
}

#[test]
fn local_store_serializes_create_only_writers() {
    let root = temporary_root();
    let store = Arc::new(LocalStore::new(&root).expect("local store"));
    let path = ObjectPath::new("objects/value").expect("object path");
    let barrier = Arc::new(Barrier::new(3));
    let mut workers = Vec::new();
    for value in [b"first".as_slice(), b"second".as_slice()] {
        let store = Arc::clone(&store);
        let path = path.clone();
        let barrier = Arc::clone(&barrier);
        let value = value.to_vec();
        workers.push(std::thread::spawn(move || {
            barrier.wait();
            store.put(&path, &value, PutCondition::CreateOnly)
        }));
    }
    barrier.wait();
    let successes = workers
        .into_iter()
        .map(|worker| worker.join().expect("writer should not panic").is_ok())
        .filter(|success| *success)
        .count();
    assert_eq!(successes, 1);
    assert!(store.get(&path, ByteSize::new(64)).is_ok());
    let _ = std::fs::remove_dir_all(root);
}

#[test]
fn local_store_verifies_ranges_and_reserves_internal_namespace() {
    let root = temporary_root();
    let store = LocalStore::new(&root).expect("local store");
    let path = ObjectPath::new("objects/value").expect("object path");
    store
        .put(&path, b"abcdef", PutCondition::CreateOnly)
        .expect("object publish");
    std::fs::write(root.join(path.as_str()), b"abcxef").expect("fixture tamper");
    let range = ByteRange::new(0, 3).expect("range");
    assert!(store.get_range(&path, range).is_err());

    let reserved = ObjectPath::new(".mindclade-object-metadata/injected").expect("object path");
    assert!(store.put(&reserved, b"bad", PutCondition::Any).is_err());
    let _ = std::fs::remove_dir_all(root);
}
