// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_object_store::{MemoryStore, ObjectPath, ObjectStore, PutCondition};
#[test]
fn create_only_is_enforced() {
    let s = MemoryStore::new();
    let p = ObjectPath::new("x").unwrap();
    assert!(s.put(&p, b"a", PutCondition::CreateOnly).is_ok());
    assert!(s.put(&p, b"b", PutCondition::CreateOnly).is_err());
}
