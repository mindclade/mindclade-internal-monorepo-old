//! Framework-independent online inference request core.

use crate::{
    AdmissionLedger, AdmissionPermit, AdmissionRequest, GatewayConfig, GatewayHealth,
    PolicyCache, RouteRequest, bounded_stream, select_route,
};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_serving_runtime::InferenceRequest;
use mindclade_worker_protocol::{AdmissionGrant, DeploymentRoute};
use std::sync::Arc;

#[derive(Clone, Debug)]
pub struct InferenceEnvelope {
    pub request_id: ResourceId,
    pub grant: AdmissionGrant,
    pub admission: AdmissionRequest,
}

pub struct AdmittedRequest {
    pub request_id: ResourceId,
    pub route: DeploymentRoute,
    pub permit: AdmissionPermit,
    pub response_tx: crate::StreamSender,
    pub response_rx: crate::StreamReceiver,
}

pub struct GatewayCore {
    config: GatewayConfig,
    policy: Arc<PolicyCache>,
    admission: AdmissionLedger,
    health: Arc<GatewayHealth>,
}

impl GatewayCore {
    pub fn new(config: GatewayConfig, policy: Arc<PolicyCache>, health: Arc<GatewayHealth>) -> FaultResult<Self> {
        config.validate()?;
        let admission = AdmissionLedger::new(config.maximum_active_requests, config.maximum_active_requests_per_grant)?;
        Ok(Self { config, policy, admission, health })
    }
    pub fn admit_request(&self, request: InferenceRequest, now_unix_millis: u64) -> FaultResult<AdmittedRequest> {
        self.admit(InferenceEnvelope { request_id: request.request_id, grant: request.grant, admission: request.admission }, now_unix_millis)
    }
    pub fn admit(&self, envelope: InferenceEnvelope, now_unix_millis: u64) -> FaultResult<AdmittedRequest> {
        if envelope.request_id.kind() != "request" {
            return Err(Fault::invalid_argument("online inference request id has the wrong kind"));
        }
        if !self.health.snapshot().accepting {
            return Err(Fault::new(Code::Unavailable, "runtime gateway is draining or not accepting"));
        }
        envelope.admission.validate(self.config.maximum_request_key_bytes)?;
        self.policy.validate_grant(&envelope.grant, now_unix_millis)?;
        let policy = self.policy.snapshot(now_unix_millis)?;
        self.health.set_policy_fresh(true);
        let route = select_route(RouteRequest {
            admission: &envelope.admission,
            grant: &envelope.grant.claims,
            snapshot: &policy.route,
            revocations: &policy.revocations,
            now_unix_millis,
        })?;
        let permit = self.admission.reserve(&envelope.grant.claims, &envelope.admission)?;
        let output_limit = if envelope.grant.claims.maximum_output_units == 0 {
            self.config.maximum_stream_output_bytes
        } else {
            self.config.maximum_stream_output_bytes.min(envelope.grant.claims.maximum_output_units)
        };
        let (response_tx, response_rx) = bounded_stream(
            self.config.maximum_stream_chunks,
            self.config.maximum_stream_chunk_bytes,
            output_limit,
        )?;
        Ok(AdmittedRequest { request_id: envelope.request_id, route, permit, response_tx, response_rx })
    }
    pub fn begin_drain(&self) { self.health.set_accepting(false); }
    pub fn resume_admission(&self) { self.health.set_accepting(true); }
    #[must_use] pub fn active_requests(&self) -> u32 { self.admission.active() }
    #[must_use] pub fn health_snapshot(&self) -> crate::GatewayHealthSnapshot { self.health.snapshot() }
}
