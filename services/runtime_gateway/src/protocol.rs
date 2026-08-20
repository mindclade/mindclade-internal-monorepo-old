// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Protobuf transport conversion into authoritative bounded runtime types.

use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_protocols::runtime::v1 as wire;
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector};
use mindclade_serving_runtime::{AdmissionRequest, InferenceRequest};
use mindclade_worker_protocol::{
    AdmissionGrant, AdmissionGrantClaims, ArtifactGrant, DeploymentRoute, DetachedSignature,
    ExecutionBudget, ExecutionTicket, ExecutionTicketClaims, RevocationSnapshot,
    RevocationSnapshotClaims, RouteSnapshot, RouteSnapshotClaims,
};
use std::collections::BTreeSet;
use std::str::FromStr;

pub fn inference_request(message: wire::RuntimeDispatchRequest) -> FaultResult<InferenceRequest> {
    let grant =
        admission_grant(message.grant.ok_or_else(|| {
            Fault::invalid_argument("dispatch request is missing admission grant")
        })?)?;
    let request_id = ResourceId::parse(&message.request_id).map_err(|error| {
        Fault::invalid_argument("dispatch request id is invalid").with_source(error)
    })?;
    let deployment_hint = nonempty_optional(message.deployment_hint);
    let payload_descriptor = nonempty_optional(message.payload_descriptor);
    Ok(InferenceRequest {
        request_id,
        grant,
        admission: AdmissionRequest {
            request_key: message.request_key,
            deployment_hint,
            required_capabilities: message
                .required_capabilities
                .into_iter()
                .collect::<BTreeSet<_>>(),
            input_units: message.input_units,
            output_units: message.output_units,
        },
        payload_descriptor,
    })
}

pub fn admission_grant(message: wire::AdmissionGrant) -> FaultResult<AdmissionGrant> {
    let claims = message
        .claims
        .ok_or_else(|| Fault::invalid_argument("admission grant claims are missing"))?;
    let signature = message
        .signature
        .ok_or_else(|| Fault::invalid_argument("admission grant signature is missing"))?;
    Ok(AdmissionGrant {
        claims: AdmissionGrantClaims {
            grant_id: parse_id(&claims.grant_id, "grant")?,
            tenant_id: parse_id(&claims.tenant_id, "tenant")?,
            principal_id: claims.principal_id,
            allowed_deployments: claims.allowed_deployments.into_iter().collect(),
            allowed_capabilities: claims.allowed_capabilities.into_iter().collect(),
            region: claims.region,
            maximum_concurrency: claims.maximum_concurrency,
            maximum_requests: claims.maximum_requests,
            maximum_input_units: claims.maximum_input_units,
            maximum_output_units: claims.maximum_output_units,
            not_before_unix_millis: claims.not_before_unix_millis,
            expires_unix_millis: claims.expires_unix_millis,
            policy_epoch: claims.policy_epoch,
            revocation_epoch: claims.revocation_epoch,
        },
        signature: DetachedSignature {
            algorithm: signature.algorithm,
            key_id: signature.key_id,
            value: signature.value,
        },
    })
}

pub fn execution_ticket(message: wire::ExecutionTicket) -> FaultResult<ExecutionTicket> {
    let claims = message
        .claims
        .ok_or_else(|| Fault::invalid_argument("execution ticket claims are missing"))?;
    let signature = message
        .signature
        .ok_or_else(|| Fault::invalid_argument("execution ticket signature is missing"))?;
    let artifacts = claims
        .artifacts
        .ok_or_else(|| Fault::invalid_argument("execution ticket artifact grant is missing"))?;
    let budget = claims
        .budget
        .ok_or_else(|| Fault::invalid_argument("execution ticket budget is missing"))?;
    Ok(ExecutionTicket {
        claims: ExecutionTicketClaims {
            ticket_id: parse_id(&claims.ticket_id, "ticket")?,
            issuer: claims.issuer,
            tenant_id: parse_id(&claims.tenant_id, "tenant")?,
            workspace_id: parse_id(&claims.workspace_id, "workspace")?,
            run_id: parse_optional_id(&claims.run_id, "run")?,
            job_id: parse_optional_id(&claims.job_id, "job")?,
            stage_id: parse_optional_id(&claims.stage_id, "stage")?,
            request_id: parse_optional_id(&claims.request_id, "request")?,
            attempt: claims.attempt,
            fencing_token: FencingToken::new(claims.fencing_token)?,
            model_bundle: parse_optional_digest(&claims.model_bundle_digest)?,
            engine_bundle: parse_optional_digest(&claims.engine_bundle_digest)?,
            resolved_config_digest: digest(&claims.resolved_config_digest)?,
            reference_snapshot: parse_optional_digest(&claims.reference_snapshot_digest)?,
            artifacts: artifact_grant(artifacts)?,
            budget: execution_budget(&budget),
            execution_class: claims.execution_class,
            accelerator_capability: claims.accelerator_capability,
            not_before_unix_millis: claims.not_before_unix_millis,
            deadline_unix_millis: claims.deadline_unix_millis,
            expires_unix_millis: claims.expires_unix_millis,
            policy_epoch: claims.policy_epoch,
            route_snapshot_version: claims.route_snapshot_version,
            revocation_epoch: claims.revocation_epoch,
            idempotency_key: claims.idempotency_key,
        },
        signature: detached_signature(signature),
    })
}

fn execution_budget(message: &wire::ExecutionBudget) -> ExecutionBudget {
    let resources = ResourceVector::new()
        .set(ResourceKind::CpuMillis, u64::from(message.cpu_millis))
        .set(
            ResourceKind::ResidentMemoryBytes,
            message.resident_memory_bytes,
        )
        .set(ResourceKind::PinnedMemoryBytes, message.pinned_memory_bytes)
        .set(ResourceKind::SharedMemoryBytes, message.shared_memory_bytes)
        .set(ResourceKind::LocalDiskBytes, message.local_disk_bytes)
        .set(
            ResourceKind::OpenFileDescriptors,
            u64::from(message.open_file_descriptors),
        )
        .set(
            ResourceKind::ObjectStoreRequests,
            u64::from(message.object_store_requests),
        )
        .set(
            ResourceKind::QueuedRequests,
            u64::from(message.queued_operations),
        )
        .set(ResourceKind::Processes, u64::from(message.child_processes))
        .set(
            ResourceKind::CpuThreads,
            u64::from(message.cpu_worker_threads),
        )
        .set(
            ResourceKind::GpuMemoryEstimateBytes,
            message.gpu_memory_estimate_bytes,
        )
        .set(
            ResourceKind::CheckpointStagingBytes,
            message.checkpoint_staging_bytes,
        )
        .set(
            ResourceKind::TelemetrySpoolBytes,
            message.telemetry_spool_bytes,
        )
        .set(
            ResourceKind::MaximumOutputBytes,
            message.maximum_output_bytes,
        );
    ExecutionBudget {
        resources,
        maximum_output_bytes: message.maximum_output_bytes,
    }
}

fn artifact_grant(message: wire::ArtifactGrant) -> FaultResult<ArtifactGrant> {
    Ok(ArtifactGrant {
        readable_digests: message
            .readable_digests
            .into_iter()
            .map(|value| digest(&value))
            .collect::<FaultResult<BTreeSet<_>>>()?,
        writable_namespaces: message.writable_namespaces.into_iter().collect(),
        maximum_read_bytes: message.maximum_read_bytes,
        maximum_write_bytes: message.maximum_write_bytes,
        allow_range_reads: message.allow_range_reads,
        allow_multipart_writes: message.allow_multipart_writes,
    })
}

pub fn digest(value: &str) -> FaultResult<Digest> {
    Digest::from_str(value)
        .map_err(|error| Fault::invalid_argument("digest is invalid").with_source(error))
}

fn parse_id(value: &str, kind: &str) -> FaultResult<ResourceId> {
    let id = ResourceId::parse(value)
        .map_err(|error| Fault::invalid_argument("resource id is invalid").with_source(error))?;
    if id.kind() != kind {
        return Err(Fault::invalid_argument("resource id has unexpected kind"));
    }
    Ok(id)
}

fn parse_optional_id(value: &str, kind: &str) -> FaultResult<Option<ResourceId>> {
    if value.is_empty() {
        Ok(None)
    } else {
        parse_id(value, kind).map(Some)
    }
}

fn parse_optional_digest(value: &str) -> FaultResult<Option<Digest>> {
    if value.is_empty() {
        Ok(None)
    } else {
        digest(value).map(Some)
    }
}

fn nonempty_optional(value: String) -> Option<String> {
    if value.is_empty() { None } else { Some(value) }
}

pub fn route_snapshot(message: wire::RouteSnapshot) -> FaultResult<RouteSnapshot> {
    let claims = message
        .claims
        .ok_or_else(|| Fault::invalid_argument("route snapshot claims are missing"))?;
    let signature = message
        .signature
        .ok_or_else(|| Fault::invalid_argument("route snapshot signature is missing"))?;
    let routes = claims
        .routes
        .into_iter()
        .map(deployment_route)
        .collect::<FaultResult<Vec<_>>>()?;
    Ok(RouteSnapshot {
        claims: RouteSnapshotClaims {
            snapshot_id: parse_id(&claims.snapshot_id, "routesnap")?,
            snapshot_digest: digest(&claims.snapshot_digest)?,
            version: claims.version,
            policy_epoch: claims.policy_epoch,
            revocation_epoch: claims.revocation_epoch,
            created_unix_millis: claims.created_unix_millis,
            expires_unix_millis: claims.expires_unix_millis,
            routes,
            minimum_runtime_version: claims.minimum_runtime_version,
        },
        signature: detached_signature(signature),
    })
}

pub fn revocation_snapshot(message: wire::RevocationSnapshot) -> FaultResult<RevocationSnapshot> {
    let claims = message
        .claims
        .ok_or_else(|| Fault::invalid_argument("revocation snapshot claims are missing"))?;
    let signature = message
        .signature
        .ok_or_else(|| Fault::invalid_argument("revocation snapshot signature is missing"))?;
    Ok(RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: claims.epoch,
            created_unix_millis: claims.created_unix_millis,
            expires_unix_millis: claims.expires_unix_millis,
            revoked_grant_ids: claims.revoked_grant_ids.into_iter().collect(),
            revoked_ticket_ids: claims.revoked_ticket_ids.into_iter().collect(),
            revoked_deployment_ids: claims.revoked_deployment_ids.into_iter().collect(),
            revoked_bundle_digests: claims.revoked_bundle_digests.into_iter().collect(),
        },
        signature: detached_signature(signature),
    })
}

fn deployment_route(message: wire::DeploymentRoute) -> FaultResult<DeploymentRoute> {
    Ok(DeploymentRoute {
        deployment_id: parse_id(&message.deployment_id, "deployment")?,
        model_bundle: digest(&message.model_bundle_digest)?,
        engine_bundle: digest(&message.engine_bundle_digest)?,
        endpoint: message.endpoint,
        region: message.region,
        weight: message.weight,
        capabilities: message.capabilities.into_iter().collect(),
        lease_expires_unix_millis: message.lease_expires_unix_millis,
        safety_policy: if message.safety_policy_digest.is_empty() {
            None
        } else {
            Some(digest(&message.safety_policy_digest)?)
        },
    })
}

fn detached_signature(signature: wire::DetachedSignature) -> DetachedSignature {
    DetachedSignature {
        algorithm: signature.algorithm,
        key_id: signature.key_id,
        value: signature.value,
    }
}
