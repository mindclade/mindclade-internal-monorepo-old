// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use bytes::Bytes;
use mindclade_content_digest::hash_bytes;
use mindclade_faults::Code;
use mindclade_object_store::{ClientConfig, Namespace, ObjectPath, adapters::arrow::ArrowProvider};

#[tokio::test]
async fn memory_provider_preserves_conditional_create_and_digest_verification() {
    let namespace = Namespace::new(ObjectPath::new("tenant/workspace").expect("namespace"));
    let provider = ArrowProvider::memory(namespace, ClientConfig::default()).expect("provider");
    let bytes = Bytes::from_static(b"provider-payload");
    let digest = hash_bytes(&bytes);

    provider
        .put_create("objects/payload", bytes.clone())
        .await
        .expect("initial create");
    let duplicate = provider
        .put_create("objects/payload", bytes.clone())
        .await
        .expect_err("conditional create must reject existing object");
    assert_eq!(duplicate.code(), Code::AlreadyExists);

    let loaded = provider
        .get("objects/payload", Some(digest))
        .await
        .expect("verified read");
    assert_eq!(loaded, bytes);
}
