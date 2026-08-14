use mindclade_artifact_cas::gc::{
    sweep, GarbageCollectionCandidate, GarbageCollectionPlan,
};
use mindclade_content_digest::hash_bytes;
use mindclade_object_store::{MemoryStore, ObjectPath, ObjectStore, PutCondition};

#[test]
fn gc_deletes_only_matching_generation() {
    let store = MemoryStore::new();
    let path = ObjectPath::new("sha256/aa/blob").expect("path");
    let bytes = b"artifact";
    let put = store
        .put(&path, bytes, PutCondition::CreateOnly)
        .expect("put");
    let plan = GarbageCollectionPlan::build([GarbageCollectionCandidate {
        digest: hash_bytes(bytes),
        path: path.clone(),
        expected_version: put.meta.version,
    }])
    .expect("plan");
    let report = sweep(&store, &plan).expect("sweep");
    assert_eq!(report.deleted, 1);
    assert!(store.head(&path).expect("head").is_none());
}

#[test]
fn gc_rejects_control_plane_plan_digest_mismatch() {
    let store = MemoryStore::new();
    let path = ObjectPath::new("sha256/aa/blob-2").expect("path");
    let bytes = b"artifact-2";
    let put = store
        .put(&path, bytes, PutCondition::CreateOnly)
        .expect("put");
    let candidate = GarbageCollectionCandidate {
        digest: hash_bytes(bytes),
        path,
        expected_version: put.meta.version,
    };
    let wrong = hash_bytes(b"wrong-plan");
    assert!(GarbageCollectionPlan::from_control_plane(wrong, [candidate]).is_err());
}
