// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use bytes::Bytes;
use mindclade_artifact_proxy::{AccessContext, ProviderArtifacts, ValidatedGrant};
use mindclade_content_digest::hash_bytes;
use mindclade_faults::Code;
use mindclade_identifiers::ResourceId;
use mindclade_object_store::{ClientConfig, Namespace, ObjectPath, adapters::arrow::ArrowProvider};
use mindclade_worker_protocol::ArtifactGrant;
use std::collections::BTreeSet;

fn resource_id(value: &str) -> ResourceId {
    ResourceId::parse(value).expect("resource id")
}

fn grant(digest: mindclade_content_digest::Digest, expires: u64) -> ValidatedGrant {
    ValidatedGrant {
        access: AccessContext {
            tenant_id: resource_id("tenant_019c0000000070008000000000000002"),
            workspace_id: resource_id("workspace_019c0000000070008000000000000003"),
            principal_id: "service-account:test".to_owned(),
        },
        grant: ArtifactGrant {
            readable_digests: BTreeSet::from([digest]),
            writable_namespaces: BTreeSet::from(["runs/test".to_owned()]),
            maximum_read_bytes: 1024,
            maximum_write_bytes: 1024,
            allow_range_reads: true,
            allow_multipart_writes: false,
        },
        ticket_id: "ticket_019c0000000070008000000000000001".to_owned(),
        expires_unix_millis: expires,
    }
}

#[tokio::test]
async fn provider_access_is_authorized_and_expiry_is_rechecked() {
    let namespace = Namespace::new(ObjectPath::new("tenant/workspace").expect("namespace"));
    let provider = ArrowProvider::memory(namespace, ClientConfig::default()).expect("provider");
    let artifacts = ProviderArtifacts::new(provider);
    let payload = Bytes::from_static(b"artifact-data");
    let digest = hash_bytes(&payload);
    let grant = grant(digest, 1_000);

    let published = artifacts
        .publish_content(&grant, "runs/test", payload.clone(), 100)
        .await
        .expect("publish");
    assert_eq!(published, digest);
    let read = artifacts
        .read_digest(&grant, digest, 200)
        .await
        .expect("read");
    assert_eq!(read, payload);

    let expired = artifacts
        .read_digest(&grant, digest, 1_000)
        .await
        .expect_err("expired grant must fail");
    assert_eq!(expired.code(), Code::DeadlineExceeded);
}
