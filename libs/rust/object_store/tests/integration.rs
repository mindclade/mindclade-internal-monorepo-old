// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_object_store::{MemoryStore, ObjectPath, ObjectStore, PutCondition};

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
