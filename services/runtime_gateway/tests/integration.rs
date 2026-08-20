// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::FaultResult;
use mindclade_identifiers::ResourceId;
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector};
use mindclade_runtime_gateway::{
    AdmissionRequest, GatewayConfig, GatewayCore, GatewayHealth, InferenceEnvelope, PolicyCache,
};
use mindclade_serving_runtime::InferenceRequest;
use mindclade_worker_protocol::{
    AdmissionGrant, AdmissionGrantClaims, ArtifactGrant, DeploymentRoute, DetachedSignature,
    ExecutionBudget, ExecutionTicket, ExecutionTicketClaims, RevocationSnapshot,
    RevocationSnapshotClaims, RouteSnapshot, RouteSnapshotClaims, SignatureVerifier,
};
use std::collections::BTreeSet;
use std::sync::Arc;

struct AcceptAll;
impl SignatureVerifier for AcceptAll {
    fn verify(&self, _payload: &[u8], _signature: &DetachedSignature) -> FaultResult<()> {
        Ok(())
    }
}

fn id(kind: &str, suffix: &str) -> ResourceId {
    format!("{kind}_01890f2c7b7a70008{suffix}")
        .parse()
        .expect("valid UUIDv7 resource id")
}
fn signature() -> DetachedSignature {
    DetachedSignature {
        algorithm: "test".into(),
        key_id: "test-key".into(),
        value: vec![1],
    }
}
fn revocations() -> RevocationSnapshot {
    RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: 1,
            created_unix_millis: 100,
            expires_unix_millis: 10_000,
            revoked_grant_ids: BTreeSet::new(),
            revoked_ticket_ids: BTreeSet::new(),
            revoked_deployment_ids: BTreeSet::new(),
            revoked_bundle_digests: BTreeSet::new(),
        },
        signature: signature(),
    }
}

fn mismatched_ticket(
    request_id: &ResourceId,
    tenant_id: &ResourceId,
    model: Digest,
) -> ExecutionTicket {
    ExecutionTicket {
        claims: ExecutionTicketClaims {
            ticket_id: id("ticket", "000000000000005"),
            issuer: "test-control-plane".to_owned(),
            tenant_id: tenant_id.clone(),
            workspace_id: id("workspace", "000000000000006"),
            run_id: None,
            job_id: None,
            stage_id: None,
            request_id: Some(request_id.clone()),
            attempt: 1,
            fencing_token: FencingToken::new(1).expect("fence"),
            model_bundle: Some(model),
            engine_bundle: Some(hash_bytes(b"wrong-engine")),
            resolved_config_digest: hash_bytes(b"config"),
            reference_snapshot: None,
            artifacts: ArtifactGrant {
                readable_digests: BTreeSet::new(),
                writable_namespaces: BTreeSet::new(),
                maximum_read_bytes: 0,
                maximum_write_bytes: 0,
                allow_range_reads: false,
                allow_multipart_writes: false,
            },
            budget: ExecutionBudget {
                resources: ResourceVector::new()
                    .set(ResourceKind::CpuMillis, 100)
                    .set(ResourceKind::ResidentMemoryBytes, 1024)
                    .set(ResourceKind::OpenFileDescriptors, 4)
                    .set(ResourceKind::CpuThreads, 1),
                maximum_output_bytes: 100,
            },
            execution_class: "online".to_owned(),
            accelerator_capability: "test".to_owned(),
            not_before_unix_millis: 100,
            deadline_unix_millis: 4_000,
            expires_unix_millis: 3_500,
            policy_epoch: 1,
            route_snapshot_version: 1,
            revocation_epoch: 1,
            idempotency_key: "quota-regression".to_owned(),
        },
        signature: signature(),
    }
}

#[test]
fn signed_local_policy_can_admit_without_control_plane_rpc() {
    let verifier: Arc<dyn SignatureVerifier> = Arc::new(AcceptAll);
    let policy = Arc::new(PolicyCache::new(verifier, revocations(), 200).expect("policy cache"));
    let deployment = id("deployment", "000000000000000");
    let model = hash_bytes(b"model");
    let engine = hash_bytes(b"engine");
    let mut claims = RouteSnapshotClaims {
        snapshot_id: id("routesnap", "000000000000001"),
        snapshot_digest: Digest::ZERO,
        version: 1,
        policy_epoch: 1,
        revocation_epoch: 1,
        created_unix_millis: 100,
        expires_unix_millis: 5_000,
        routes: vec![DeploymentRoute {
            deployment_id: deployment.clone(),
            model_bundle: model,
            engine_bundle: engine,
            endpoint: "unix:///run/mindclade/runtime-host.sock".into(),
            region: "us-central1".into(),
            weight: 100,
            capabilities: BTreeSet::from(["structure".into()]),
            lease_expires_unix_millis: 5_000,
            safety_policy: None,
        }],
        minimum_runtime_version: "1".into(),
    };
    claims.snapshot_digest = claims.computed_digest().expect("snapshot digest");
    policy
        .install_route(
            RouteSnapshot {
                claims,
                signature: signature(),
            },
            200,
        )
        .expect("route install");
    let health = Arc::new(GatewayHealth::new());
    health.set_accepting(true);
    health.set_runtime_host_ready(true);
    let gateway = GatewayCore::new(GatewayConfig::default(), policy, health).expect("gateway");
    let grant = AdmissionGrant {
        claims: AdmissionGrantClaims {
            grant_id: id("grant", "000000000000002"),
            tenant_id: id("tenant", "000000000000003"),
            principal_id: "principal:test".into(),
            allowed_deployments: BTreeSet::from([deployment.to_string()]),
            allowed_capabilities: BTreeSet::from(["structure".into()]),
            region: "us-central1".into(),
            maximum_concurrency: 2,
            maximum_requests: 1,
            maximum_input_units: 1_000,
            maximum_output_units: 1_000,
            not_before_unix_millis: 100,
            expires_unix_millis: 4_000,
            policy_epoch: 1,
            revocation_epoch: 1,
        },
        signature: signature(),
    };
    let request_id = id("request", "000000000000004");
    let admission = AdmissionRequest {
        request_key: b"stable-request-key".to_vec(),
        deployment_hint: None,
        required_capabilities: BTreeSet::from(["structure".into()]),
        input_units: 100,
        output_units: 100,
    };
    let mismatched_ticket = mismatched_ticket(&request_id, &grant.claims.tenant_id, model);
    gateway
        .admit_execution(
            InferenceRequest {
                request_id: request_id.clone(),
                grant: grant.clone(),
                admission: admission.clone(),
                payload_descriptor: None,
            },
            &mismatched_ticket,
            250,
        )
        .expect_err("route-mismatched execution ticket must fail before reservation");
    let admitted = gateway
        .admit(
            InferenceEnvelope {
                request_id,
                grant,
                admission,
            },
            250,
        )
        .expect("admitted");
    assert_eq!(admitted.route.deployment_id, deployment);
    assert_eq!(gateway.active_requests(), 1);
    drop(admitted);
    assert_eq!(gateway.active_requests(), 0);
}
