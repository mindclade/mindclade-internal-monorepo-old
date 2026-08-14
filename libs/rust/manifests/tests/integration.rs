use mindclade_content_digest::hash_bytes;
use mindclade_manifests::ArtifactRef;

#[test] fn artifact_identity_is_location_independent() {
    let a=ArtifactRef {
        digest: hash_bytes(b"x"), size_bytes: 1, media_type: "application/octet-stream".into(), logical_kind: "dataset-shard".into(), schema_version: 1
    };
    assert!(a.validate().is_ok());
}
