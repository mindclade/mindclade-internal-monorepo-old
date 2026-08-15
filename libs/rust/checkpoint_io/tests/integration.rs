// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_artifact_cas::{ArtifactCas, CasConfig};
use mindclade_checkpoint_io::{CheckpointReader, CheckpointWriter};
use mindclade_content_digest::hash_bytes;
use mindclade_identifiers::ResourceId;
use mindclade_object_store::MemoryStore;
use mindclade_runtime_core::ManualClock;
use std::sync::Arc;
use std::time::{Instant, SystemTime};

#[test]
fn commit_marker_makes_checkpoint_visible() {
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let store = Arc::new(MemoryStore::new());
    let cas = ArtifactCas::new(store.clone(), clock.clone(), CasConfig::default());
    let run = ResourceId::generate("run", clock.as_ref());
    if let (Ok(cas), Ok(run)) = (cas, run) {
        let writer = CheckpointWriter::new(cas.clone(), store.clone(), clock);
        let session = writer.begin(run, 10, 1, hash_bytes(b"plan"));
        assert!(session.is_ok());
        if let Ok(mut session) = session {
            assert!(session.write_shard("model/rank-0", 0, b"weights").is_ok());
            let id = session.checkpoint_id().to_string();
            let committed = session.commit();
            assert!(committed.is_ok());
            let reader = CheckpointReader::new(cas, store);
            assert!(reader.load(&id).is_ok());
            assert!(reader.verify(&id).is_ok_and(|report| report.is_valid()));
        }
    }
}
