use mindclade_content_digest::hash_bytes;
use mindclade_manifests::ArtifactRef;

#[test]fn artifact_ref_validates_identity_without_location() {
    let r=ArtifactRef {
        digest: hash_bytes(b"x"), size_bytes: 1, media_type: "application/octet-stream".into(), logical_kind: "dataset/shard".into(), schema_version: 1
    };
    assert!(r.validate().is_ok());
}
