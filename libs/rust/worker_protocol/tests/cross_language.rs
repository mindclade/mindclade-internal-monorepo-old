use mindclade_content_digest::Digest;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::{
    FencingToken, ResourceKind, ResourceVector
};
use mindclade_worker_protocol::*;
use std::collections::BTreeSet;
use std::str::FromStr;

fn id(s: &str) -> ResourceId {
    s.parse().expect("id")
}

fn digest(ch: char) -> Digest {
    Digest::from_str(&format!("sha256:{}", ch.to_string().repeat(64))).expect("digest")
}

#[test]
fn mcce1_ticket_matches_go_golden() {
    let claims=ExecutionTicketClaims {
        ticket_id: id("ticket_019c0000000070008000000000000001"), issuer: "control".into(), tenant_id: id("tenant_019c0000000070008000000000000002"),
        workspace_id: id("workspace_019c0000000070008000000000000003"),
        run_id: None,
        job_id: None,
        stage_id: Some(id("stage_019c0000000070008000000000000004")),
        request_id: None, attempt: 1, fencing_token: FencingToken::new(9).expect("fence"), model_bundle: Some(digest('1')),
        engine_bundle: Some(digest('2')), resolved_config_digest: digest('3'), reference_snapshot: Some(digest('4')),
        artifacts: ArtifactGrant {
            readable_digests: BTreeSet::from([digest('a')]), writable_namespaces: BTreeSet::from(["tenant/t1/run/r1".to_owned()]),
            maximum_read_bytes: 1024, maximum_write_bytes: 2048, allow_range_reads: true, allow_multipart_writes: true
        }, budget: ExecutionBudget {
            resources: ResourceVector::new().set(ResourceKind::CpuMillis, 2000).set(ResourceKind::ResidentMemoryBytes,
            8<<30).set(ResourceKind::PinnedMemoryBytes, 1<<30).set(ResourceKind::SharedMemoryBytes, 512<<20).set(ResourceKind::LocalDiskBytes,
            16<<30).set(ResourceKind::OpenFileDescriptors, 128).set(ResourceKind::ObjectStoreRequests, 16).set(ResourceKind::QueuedRequests,
            8).set(ResourceKind::Processes, 2).set(ResourceKind::CpuThreads, 8).set(ResourceKind::GpuMemoryEstimateBytes,
            40<<30).set(ResourceKind::CheckpointStagingBytes, 4<<30).set(ResourceKind::TelemetrySpoolBytes, 64<<20),
            maximum_output_bytes: 2<<30
        }, execution_class: "gpu".into(), accelerator_capability: "sm90".into(), not_before_unix_millis: 1_800_000_000_000,
        deadline_unix_millis: 1_800_000_600_000, expires_unix_millis: 1_800_000_300_000, policy_epoch: 12, route_snapshot_version: 34,
        revocation_epoch: 7, idempotency_key: "run:r1:stage:s1:attempt:1".into()
    };
    let actual=claims.canonical_bytes().expect("canonical");
    let expected=include_bytes!("../../../tests/integration/cross_language/fixtures/execution_ticket_claims_v1.bin");
    assert_eq!(actual.as_slice(), expected);
    let verifier=HmacSha256Verifier::new([("golden-hmac-v1", b"0123456789abcdef0123456789abcdef".to_vec())])
    .expect("verifier");
    let signature=DetachedSignature {
        algorithm: "hmac-sha256".into(),
        key_id: "golden-hmac-v1".into(),
        value: hex("e0e34aeacf0c286923562b78bb59329726124c97d14bed8bf9e4ee6e8a964fe5"),
    };
    assert!(verifier.verify(&actual, &signature).is_ok());
}

fn hex(value: &str) -> Vec<u8> {
    value.as_bytes().chunks_exact(2).map(|p| {
        fn n(b: u8) -> u8 {
            match b {
                b'0'..=b'9'=>b-b'0', b'a'..=b'f'=>b-b'a'+10, _=>panic!("hex")
            }
        }
        (n(p[0])<<4)|n(p[1])
    }).collect()
}
