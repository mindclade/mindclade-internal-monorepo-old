//! Protobuf transport conversion into authoritative bounded runtime types.

use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_protocols::runtime::v1 as wire;
use mindclade_serving_runtime::{AdmissionRequest, InferenceRequest};
use mindclade_worker_protocol::{
    AdmissionGrant, AdmissionGrantClaims, DeploymentRoute, DetachedSignature, RevocationSnapshot,
    RevocationSnapshotClaims, RouteSnapshot, RouteSnapshotClaims,
};
use std::collections::BTreeSet;
use std::str::FromStr;

pub fn inference_request(message: wire::RuntimeDispatchRequest) -> FaultResult<InferenceRequest> {
    let grant = admission_grant(message.grant.ok_or_else(|| Fault::invalid_argument("dispatch request is missing admission grant"))?)?;
    let request_id = ResourceId::parse(&message.request_id)
        .map_err(|error| Fault::invalid_argument("dispatch request id is invalid").with_source(error))?;
    let deployment_hint = nonempty_optional(message.deployment_hint);
    let payload_descriptor = nonempty_optional(message.payload_descriptor);
    Ok(InferenceRequest {
        request_id,
        grant,
        admission: AdmissionRequest {
            request_key: message.request_key,
            deployment_hint,
            required_capabilities: message.required_capabilities.into_iter().collect::<BTreeSet<_>>(),
            input_units: message.input_units,
            output_units: message.output_units,
        },
        payload_descriptor,
    })
}

pub fn admission_grant(message: wire::AdmissionGrant) -> FaultResult<AdmissionGrant> {
    let claims = message.claims.ok_or_else(|| Fault::invalid_argument("admission grant claims are missing"))?;
    let signature = message.signature.ok_or_else(|| Fault::invalid_argument("admission grant signature is missing"))?;
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
        signature: DetachedSignature { algorithm: signature.algorithm, key_id: signature.key_id, value: signature.value },
    })
}

pub fn digest(value: &str) -> FaultResult<Digest> {
    Digest::from_str(value).map_err(|error| Fault::invalid_argument("digest is invalid").with_source(error))
}

fn parse_id(value: &str, kind: &str) -> FaultResult<ResourceId> {
    let id = ResourceId::parse(value)
        .map_err(|error| Fault::invalid_argument("resource id is invalid").with_source(error))?;
    if id.kind() != kind {
        return Err(Fault::invalid_argument("resource id has unexpected kind"));
    }
    Ok(id)
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
