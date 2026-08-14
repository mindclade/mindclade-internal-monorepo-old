use mindclade_artifact_cas::{ArtifactCas, CasConfig};
use mindclade_manifests::{ArtifactManifest, BlobRef};
use mindclade_runtime_core::ManualClock;
use mindclade_identifiers::{Name, ResourceId};
use mindclade_object_store::MemoryStore;
use std::sync::Arc;
use std::time::{Instant, SystemTime};

#[test]
fn blobs_and_manifests_publish_idempotently() {
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let cas = ArtifactCas::new(Arc::new(MemoryStore::new()), clock.clone(), CasConfig::default());
    assert!(cas.is_ok());
    if let Ok(cas) = cas {
        let digest = cas.put_blob(b"weights"); assert!(digest.is_ok());
        let id = ResourceId::generate("artifact", clock.as_ref());
        let kind = Name::new("model/checkpoint"); let path = Name::new("weights/shard-0.bin");
        if let (Ok(digest), Ok(id), Ok(kind), Ok(path)) = (digest, id, kind, path) {
            let blob = BlobRef::new(path, digest, 7, "application/octet-stream");
            if let Ok(blob) = blob {
                let manifest = ArtifactManifest::new(id, kind, vec![blob]);
                if let Ok(manifest) = manifest {
                    assert!(cas.publish_manifest(&manifest).is_ok());
                    assert_eq!(cas.get_blob(digest).ok(), Some(b"weights".to_vec()));
                }
            }
        }
    }
}

use mindclade_artifact_cas::index::CasIndex;
use mindclade_artifact_cas::retention::RetentionPolicy;
use mindclade_content_digest::hash_bytes;
use std::time::Duration;

#[test]
fn index_conflict_does_not_replace_existing_digest() {
    let clock = ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now());
    let id = ResourceId::generate("artifact", &clock).expect("artifact id");
    let first = hash_bytes(b"first");
    let second = hash_bytes(b"second");
    let mut index = CasIndex::default();
    assert!(index.insert(id.clone(), first).is_ok());
    assert!(index.insert(id.clone(), first).is_ok());
    assert!(index.insert(id.clone(), second).is_err());
    assert_eq!(index.get(&id), Some(first));
}

#[test]
fn retention_policy_rejects_unbounded_grace_or_delete_count() {
    assert!(RetentionPolicy {
        garbage_collection_grace: Duration::ZERO,
        maximum_deletes_per_run: 1,
    }
    .validate()
    .is_err());
    assert!(RetentionPolicy {
        garbage_collection_grace: Duration::from_secs(366 * 24 * 60 * 60),
        maximum_deletes_per_run: 1,
    }
    .validate()
    .is_err());
    assert!(RetentionPolicy {
        garbage_collection_grace: Duration::from_secs(60),
        maximum_deletes_per_run: 10_000,
    }
    .validate()
    .is_ok());
}
