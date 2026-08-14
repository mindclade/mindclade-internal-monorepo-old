use mindclade_content_digest::hash_bytes;
use mindclade_artifact_cas::blob::blob_path;
#[test]fn digest_path_is_content_addressed() {
    let a=blob_path(hash_bytes(b"a")).unwrap();
    let b=blob_path(hash_bytes(b"b")).unwrap();
    assert_ne!(a, b);
    assert!(a.as_str().starts_with("cas/blobs/sha256/"));
}
