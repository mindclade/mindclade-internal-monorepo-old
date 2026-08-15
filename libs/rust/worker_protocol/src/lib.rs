// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Validated runtime policy/worker views and MCCE1 canonical claim encoding.
//!
//! These types are the Rust projection of the cross-language runtime authority
//! contract. They are deliberately dependency-light and bounded because signed
//! policy objects are consumed on latency-sensitive and privilege-sensitive
//! runtime paths.
#![forbid(unsafe_code)]

pub mod command;
pub mod sequence;
pub mod signing;
pub mod status;
pub mod ticket;
pub mod validation;
pub mod workload;
use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector};
// Digest and ResourceId appear in the public fields of the types below, so callers already have
// to name them; without these re-exports they can only do that by depending on
// mindclade_content_digest and mindclade_identifiers directly. architecture/dependency_budgets.toml
// deliberately does not permit that for services/runtime_host — the wire contract is meant to be
// the single seam. Re-exporting here is what makes that boundary followable rather than a rule
// consumers have to break to compile.
pub use mindclade_content_digest::Digest;
pub use mindclade_identifiers::ResourceId;
pub use signing::{Ed25519KeySet, Ed25519VerificationKey};

use std::collections::{BTreeMap, BTreeSet};
pub use workload::{WorkloadEnvelope, WorkloadKind};

const MAX_CANONICAL_BYTES: usize = 16 * 1024 * 1024;
const MAX_SET_ENTRIES: usize = 65_536;
const MAX_ROUTES: usize = 16_384;
const MAX_REVOCATIONS_PER_CLASS: usize = MAX_SET_ENTRIES;
const MAX_CAPABILITIES: usize = 256;
const MAX_INPUT_DIMENSIONS: usize = 16;
const MAX_SIGNATURE_BYTES: usize = 4096;
const MAX_KEY_ID_BYTES: usize = 256;
const MAX_SHORT_TEXT_BYTES: usize = 4096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DetachedSignature {
    pub algorithm: String,
    pub key_id: String,
    pub value: Vec<u8>,
}
impl DetachedSignature {
    pub fn validate(&self) -> FaultResult<()> {
        if self.algorithm.is_empty()
            || self.algorithm.len() > 128
            || self.key_id.is_empty()
            || self.key_id.len() > MAX_KEY_ID_BYTES
            || self.value.is_empty()
            || self.value.len() > MAX_SIGNATURE_BYTES
        {
            return Err(Fault::new(
                Code::Unauthenticated,
                "detached signature is invalid",
            ));
        }
        Ok(())
    }
}

pub trait SignatureVerifier: Send + Sync {
    fn verify(&self, payload: &[u8], signature: &DetachedSignature) -> FaultResult<()>;
}

/// Dependency-free reference HMAC-SHA256 verifier used by cross-language
/// qualification and deployments that deliberately choose symmetric runtime
/// signing. Production asymmetric/KMS verification is supplied by a leaf adapter.
#[derive(Clone, Debug, Default)]
pub struct HmacSha256Verifier {
    keys: BTreeMap<String, Vec<u8>>,
}
impl HmacSha256Verifier {
    pub fn new<I, K, V>(keys: I) -> FaultResult<Self>
    where
        I: IntoIterator<Item = (K, V)>,
        K: Into<String>,
        V: Into<Vec<u8>>,
    {
        let mut output = BTreeMap::new();
        for (key_id, key_bytes) in keys {
            let key_id = key_id.into();
            let key_bytes = key_bytes.into();
            if key_id.is_empty()
                || key_id.len() > MAX_KEY_ID_BYTES
                || key_bytes.len() < 32
                || key_bytes.len() > 64 * 1024
            {
                return Err(Fault::invalid_argument(
                    "HMAC verification key is outside policy bounds",
                ));
            }
            if output.len() >= 1024 {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "HMAC keyset exceeds bound",
                ));
            }
            if output.insert(key_id, key_bytes).is_some() {
                return Err(Fault::new(
                    Code::AlreadyExists,
                    "duplicate HMAC verification key",
                ));
            }
        }
        if output.is_empty() {
            return Err(Fault::invalid_argument(
                "HMAC verifier requires at least one key",
            ));
        }
        Ok(Self { keys: output })
    }
}
impl SignatureVerifier for HmacSha256Verifier {
    fn verify(&self, payload: &[u8], signature: &DetachedSignature) -> FaultResult<()> {
        signature.validate()?;
        if payload.len() > MAX_CANONICAL_BYTES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "signed payload exceeds canonical bound",
            ));
        }
        if signature.algorithm != "hmac-sha256" {
            return Err(Fault::new(
                Code::Unauthenticated,
                "signature algorithm is not accepted by HMAC verifier",
            ));
        }
        let key = self
            .keys
            .get(&signature.key_id)
            .ok_or_else(|| Fault::new(Code::Unauthenticated, "signature key is unknown"))?;
        let expected = hmac_sha256(key, payload);
        if constant_time_eq(&expected, &signature.value) {
            Ok(())
        } else {
            Err(Fault::new(
                Code::Unauthenticated,
                "signature verification failed",
            ))
        }
    }
}

fn hmac_sha256(key: &[u8], payload: &[u8]) -> [u8; 32] {
    let mut block = [0_u8; 64];
    if key.len() > 64 {
        block[..32].copy_from_slice(hash_bytes(key).as_bytes());
    } else {
        block[..key.len()].copy_from_slice(key);
    }
    let mut inner = Vec::with_capacity(64 + payload.len());
    let mut outer = Vec::with_capacity(96);
    for byte in block {
        inner.push(byte ^ 0x36);
    }
    inner.extend_from_slice(payload);
    let inner_digest = hash_bytes(&inner);
    for byte in block {
        outer.push(byte ^ 0x5c);
    }
    outer.extend_from_slice(inner_digest.as_bytes());
    *hash_bytes(&outer).as_bytes()
}

fn constant_time_eq(left: &[u8], right: &[u8]) -> bool {
    if left.len() != right.len() {
        return false;
    }
    let mut difference = 0_u8;
    for (left, right) in left.iter().zip(right) {
        difference |= *left ^ *right;
    }
    difference == 0
}

struct CanonicalEncoder {
    bytes: Vec<u8>,
}
impl CanonicalEncoder {
    fn new(kind: &str) -> FaultResult<Self> {
        if kind.is_empty() || kind.len() > 128 {
            return Err(Fault::invalid_argument("canonical object kind is invalid"));
        }
        let mut bytes = Vec::with_capacity(128);
        bytes.extend_from_slice(b"MCCE1/");
        bytes.extend_from_slice(kind.as_bytes());
        bytes.push(0);
        Ok(Self { bytes })
    }
    fn field(&mut self, key: &str, value: &[u8]) -> FaultResult<()> {
        let key_len = u16::try_from(key.len())
            .map_err(|_| Fault::new(Code::ResourceExhausted, "canonical field key exceeds u16"))?;
        let value_len = u32::try_from(value.len()).map_err(|_| {
            Fault::new(Code::ResourceExhausted, "canonical field value exceeds u32")
        })?;
        let additional = 2_usize
            .checked_add(key.len())
            .and_then(|size| size.checked_add(4))
            .and_then(|size| size.checked_add(value.len()))
            .ok_or_else(|| Fault::new(Code::OutOfRange, "canonical size accounting overflow"))?;
        let next =
            self.bytes.len().checked_add(additional).ok_or_else(|| {
                Fault::new(Code::OutOfRange, "canonical size accounting overflow")
            })?;
        if next > MAX_CANONICAL_BYTES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "canonical object exceeds size bound",
            ));
        }
        self.bytes.extend_from_slice(&key_len.to_be_bytes());
        self.bytes.extend_from_slice(key.as_bytes());
        self.bytes.extend_from_slice(&value_len.to_be_bytes());
        self.bytes.extend_from_slice(value);
        Ok(())
    }
    fn text(&mut self, key: &str, value: &str) -> FaultResult<()> {
        self.field(key, value.as_bytes())
    }
    fn u64(&mut self, key: &str, value: u64) -> FaultResult<()> {
        self.field(key, &value.to_be_bytes())
    }
    fn u32(&mut self, key: &str, value: u32) -> FaultResult<()> {
        self.field(key, &value.to_be_bytes())
    }
    fn boolean(&mut self, key: &str, value: bool) -> FaultResult<()> {
        self.field(key, &[u8::from(value)])
    }
    fn nested(&mut self, key: &str, value: &[u8]) -> FaultResult<()> {
        self.field(key, value)
    }
    fn strings<I, S>(&mut self, key: &str, values: I) -> FaultResult<()>
    where
        I: IntoIterator<Item = S>,
        S: AsRef<str>,
    {
        let mut values: Vec<String> = values
            .into_iter()
            .map(|value| value.as_ref().to_owned())
            .collect();
        if values.len() > MAX_SET_ENTRIES {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "canonical string set exceeds entry bound",
            ));
        }
        values.sort();
        let count = u32::try_from(values.len())
            .map_err(|_| Fault::new(Code::ResourceExhausted, "canonical string set exceeds u32"))?;
        let mut bytes = Vec::new();
        bytes.extend_from_slice(&count.to_be_bytes());
        for value in values {
            let length = u32::try_from(value.len())
                .map_err(|_| Fault::new(Code::ResourceExhausted, "canonical string exceeds u32"))?;
            let next = bytes
                .len()
                .checked_add(4)
                .and_then(|size| size.checked_add(value.len()))
                .ok_or_else(|| {
                    Fault::new(Code::OutOfRange, "canonical string-set size overflow")
                })?;
            if next > MAX_CANONICAL_BYTES {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "canonical string set exceeds size bound",
                ));
            }
            bytes.extend_from_slice(&length.to_be_bytes());
            bytes.extend_from_slice(value.as_bytes());
        }
        self.field(key, &bytes)
    }
    fn finish(self) -> Vec<u8> {
        self.bytes
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ArtifactGrant {
    pub readable_digests: BTreeSet<Digest>,
    pub writable_namespaces: BTreeSet<String>,
    pub maximum_read_bytes: u64,
    pub maximum_write_bytes: u64,
    pub allow_range_reads: bool,
    pub allow_multipart_writes: bool,
}
impl ArtifactGrant {
    pub fn validate(&self) -> FaultResult<()> {
        if self.readable_digests.len() > MAX_SET_ENTRIES || self.writable_namespaces.len() > 4096 {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "artifact grant exceeds entry bounds",
            ));
        }
        if !self.readable_digests.is_empty() && self.maximum_read_bytes == 0 {
            return Err(Fault::invalid_argument("artifact read budget is required"));
        }
        if !self.writable_namespaces.is_empty() && self.maximum_write_bytes == 0 {
            return Err(Fault::invalid_argument("artifact write budget is required"));
        }
        if self.writable_namespaces.iter().any(|value| {
            value.trim().is_empty()
                || value.len() > 1024
                || value.starts_with('/')
                || value.contains("..")
        }) {
            return Err(Fault::invalid_argument(
                "artifact writable namespace is invalid",
            ));
        }
        Ok(())
    }
    fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        self.validate()?;
        let mut encoder = CanonicalEncoder::new("artifact-grant")?;
        encoder.strings(
            "readable_digests",
            self.readable_digests.iter().map(ToString::to_string),
        )?;
        encoder.strings("writable_namespaces", self.writable_namespaces.iter())?;
        encoder.u64("maximum_read_bytes", self.maximum_read_bytes)?;
        encoder.u64("maximum_write_bytes", self.maximum_write_bytes)?;
        encoder.boolean("allow_range_reads", self.allow_range_reads)?;
        encoder.boolean("allow_multipart_writes", self.allow_multipart_writes)?;
        Ok(encoder.finish())
    }
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ExecutionBudget {
    pub resources: ResourceVector,
    pub maximum_output_bytes: u64,
}
impl ExecutionBudget {
    pub fn validate(&self) -> FaultResult<()> {
        for kind in [
            ResourceKind::CpuMillis,
            ResourceKind::OpenFileDescriptors,
            ResourceKind::ObjectStoreRequests,
            ResourceKind::QueuedRequests,
            ResourceKind::Processes,
            ResourceKind::CpuThreads,
        ] {
            if self.resources.get(kind) > u64::from(u32::MAX) {
                return Err(Fault::new(
                    Code::OutOfRange,
                    "execution budget u32 field exceeds wire range",
                )
                .with_context("resource", format!("{kind:?}")));
            }
        }
        if self.resources.get(ResourceKind::CpuMillis) == 0
            || self.resources.get(ResourceKind::ResidentMemoryBytes) == 0
            || self.resources.get(ResourceKind::OpenFileDescriptors) == 0
            || self.resources.get(ResourceKind::CpuThreads) == 0
        {
            return Err(Fault::invalid_argument(
                "execution budget requires CPU, resident memory, FD, and thread limits",
            ));
        }
        Ok(())
    }
    #[must_use]
    pub fn to_resources(&self) -> ResourceVector {
        let mut resources = self.resources.clone();
        if self.maximum_output_bytes > 0 {
            resources = resources.set(ResourceKind::MaximumOutputBytes, self.maximum_output_bytes);
        }
        resources
    }
    // Public to match ExecutionTicket, WorkloadEnvelope, and the other wire types, whose
    // canonical_bytes is already public. The canonical encoding is the signed form — being able
    // to reproduce it is part of the contract, not an internal detail.
    pub fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        self.validate()?;
        let u32_value = |kind: ResourceKind| -> FaultResult<u32> {
            u32::try_from(self.resources.get(kind)).map_err(|_| {
                Fault::new(Code::OutOfRange, "execution budget exceeds u32 wire range")
            })
        };
        let mut encoder = CanonicalEncoder::new("execution-budget")?;
        encoder.u32("cpu_millis", u32_value(ResourceKind::CpuMillis)?)?;
        encoder.u64(
            "resident_memory_bytes",
            self.resources.get(ResourceKind::ResidentMemoryBytes),
        )?;
        encoder.u64(
            "pinned_memory_bytes",
            self.resources.get(ResourceKind::PinnedMemoryBytes),
        )?;
        encoder.u64(
            "shared_memory_bytes",
            self.resources.get(ResourceKind::SharedMemoryBytes),
        )?;
        encoder.u64(
            "local_disk_bytes",
            self.resources.get(ResourceKind::LocalDiskBytes),
        )?;
        encoder.u32(
            "open_file_descriptors",
            u32_value(ResourceKind::OpenFileDescriptors)?,
        )?;
        encoder.u32(
            "object_store_requests",
            u32_value(ResourceKind::ObjectStoreRequests)?,
        )?;
        encoder.u32(
            "queued_operations",
            u32_value(ResourceKind::QueuedRequests)?,
        )?;
        encoder.u32("child_processes", u32_value(ResourceKind::Processes)?)?;
        encoder.u32("cpu_worker_threads", u32_value(ResourceKind::CpuThreads)?)?;
        encoder.u64(
            "gpu_memory_estimate_bytes",
            self.resources.get(ResourceKind::GpuMemoryEstimateBytes),
        )?;
        encoder.u64(
            "checkpoint_staging_bytes",
            self.resources.get(ResourceKind::CheckpointStagingBytes),
        )?;
        encoder.u64(
            "telemetry_spool_bytes",
            self.resources.get(ResourceKind::TelemetrySpoolBytes),
        )?;
        encoder.u64("maximum_output_bytes", self.maximum_output_bytes)?;
        Ok(encoder.finish())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExecutionTicketClaims {
    pub ticket_id: ResourceId,
    pub issuer: String,
    pub tenant_id: ResourceId,
    pub workspace_id: ResourceId,
    pub run_id: Option<ResourceId>,
    pub job_id: Option<ResourceId>,
    pub stage_id: Option<ResourceId>,
    pub request_id: Option<ResourceId>,
    pub attempt: u32,
    pub fencing_token: FencingToken,
    pub model_bundle: Option<Digest>,
    pub engine_bundle: Option<Digest>,
    pub resolved_config_digest: Digest,
    pub reference_snapshot: Option<Digest>,
    pub artifacts: ArtifactGrant,
    pub budget: ExecutionBudget,
    pub execution_class: String,
    pub accelerator_capability: String,
    pub not_before_unix_millis: u64,
    pub deadline_unix_millis: u64,
    pub expires_unix_millis: u64,
    pub policy_epoch: u64,
    pub route_snapshot_version: u64,
    pub revocation_epoch: u64,
    pub idempotency_key: String,
}
impl ExecutionTicketClaims {
    pub fn validate_static(&self) -> FaultResult<()> {
        if self.ticket_id.kind() != "ticket"
            || self.tenant_id.kind() != "tenant"
            || self.workspace_id.kind() != "workspace"
            || self.issuer.is_empty()
            || self.issuer.len() > 256
            || self.attempt == 0
            || self.execution_class.is_empty()
            || self.execution_class.len() > 128
            || self.accelerator_capability.len() > 256
            || self.idempotency_key.len() > 1024
            || self.policy_epoch == 0
            || self.route_snapshot_version == 0
            || self.revocation_epoch == 0
        {
            return Err(Fault::invalid_argument(
                "execution ticket claims are incomplete or outside bounds",
            ));
        }
        for (value, kind) in [
            (self.run_id.as_ref(), "run"),
            (self.job_id.as_ref(), "job"),
            (self.stage_id.as_ref(), "stage"),
            (self.request_id.as_ref(), "request"),
        ] {
            if value.is_some_and(|id| id.kind() != kind) {
                return Err(Fault::invalid_argument(
                    "execution ticket contains wrong resource-id kind",
                ));
            }
        }
        if self.run_id.is_none()
            && self.job_id.is_none()
            && self.stage_id.is_none()
            && self.request_id.is_none()
        {
            return Err(Fault::invalid_argument(
                "execution ticket has no work identity",
            ));
        }
        if self.not_before_unix_millis >= self.expires_unix_millis
            || self.expires_unix_millis > self.deadline_unix_millis
        {
            return Err(Fault::invalid_argument(
                "execution ticket time window is invalid",
            ));
        }
        self.artifacts.validate()?;
        self.budget.validate()
    }
    pub fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        self.validate_static()?;
        let mut encoder = CanonicalEncoder::new("execution-ticket-claims")?;
        encoder.text("ticket_id", &self.ticket_id.to_string())?;
        encoder.text("issuer", &self.issuer)?;
        encoder.text("tenant_id", &self.tenant_id.to_string())?;
        encoder.text("workspace_id", &self.workspace_id.to_string())?;
        encoder.text(
            "run_id",
            &self
                .run_id
                .as_ref()
                .map(ToString::to_string)
                .unwrap_or_default(),
        )?;
        encoder.text(
            "job_id",
            &self
                .job_id
                .as_ref()
                .map(ToString::to_string)
                .unwrap_or_default(),
        )?;
        encoder.text(
            "stage_id",
            &self
                .stage_id
                .as_ref()
                .map(ToString::to_string)
                .unwrap_or_default(),
        )?;
        encoder.text(
            "request_id",
            &self
                .request_id
                .as_ref()
                .map(ToString::to_string)
                .unwrap_or_default(),
        )?;
        encoder.u32("attempt", self.attempt)?;
        encoder.u64("fencing_token", self.fencing_token.get())?;
        encoder.text(
            "model_bundle_digest",
            &self
                .model_bundle
                .map(|value| value.to_string())
                .unwrap_or_default(),
        )?;
        encoder.text(
            "engine_bundle_digest",
            &self
                .engine_bundle
                .map(|value| value.to_string())
                .unwrap_or_default(),
        )?;
        encoder.text(
            "resolved_config_digest",
            &self.resolved_config_digest.to_string(),
        )?;
        encoder.text(
            "reference_snapshot_digest",
            &self
                .reference_snapshot
                .map(|value| value.to_string())
                .unwrap_or_default(),
        )?;
        encoder.nested("artifact_grant", &self.artifacts.canonical_bytes()?)?;
        encoder.nested("budget", &self.budget.canonical_bytes()?)?;
        encoder.text("execution_class", &self.execution_class)?;
        encoder.text("accelerator_capability", &self.accelerator_capability)?;
        encoder.u64("not_before_unix_millis", self.not_before_unix_millis)?;
        encoder.u64("deadline_unix_millis", self.deadline_unix_millis)?;
        encoder.u64("expires_unix_millis", self.expires_unix_millis)?;
        encoder.u64("policy_epoch", self.policy_epoch)?;
        encoder.u64("route_snapshot_version", self.route_snapshot_version)?;
        encoder.u64("revocation_epoch", self.revocation_epoch)?;
        encoder.text("idempotency_key", &self.idempotency_key)?;
        Ok(encoder.finish())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExecutionTicket {
    pub claims: ExecutionTicketClaims,
    pub signature: DetachedSignature,
}
impl ExecutionTicket {
    pub fn validate<V: SignatureVerifier + ?Sized>(
        &self,
        now: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<()> {
        revocations.validate(now, self.claims.revocation_epoch, verifier)?;
        let bytes = self.claims.canonical_bytes()?;
        self.signature.validate()?;
        verifier.verify(&bytes, &self.signature)?;
        if now < self.claims.not_before_unix_millis
            || now >= self.claims.expires_unix_millis
            || now > self.claims.deadline_unix_millis
        {
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "execution ticket is not active",
            ));
        }
        if self.claims.policy_epoch < minimum_policy_epoch
            || self.claims.route_snapshot_version < minimum_route_version
            || self.claims.revocation_epoch < revocations.claims.epoch
        {
            return Err(Fault::new(
                Code::PermissionDenied,
                "execution ticket policy state is stale",
            ));
        }
        if revocations.ticket_revoked(&self.claims.ticket_id.to_string())
            || self
                .claims
                .model_bundle
                .is_some_and(|digest| revocations.bundle_revoked(&digest.to_string()))
            || self
                .claims
                .engine_bundle
                .is_some_and(|digest| revocations.bundle_revoked(&digest.to_string()))
        {
            return Err(Fault::new(
                Code::PermissionDenied,
                "execution ticket is revoked",
            ));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdmissionGrantClaims {
    pub grant_id: ResourceId,
    pub tenant_id: ResourceId,
    pub principal_id: String,
    pub allowed_deployments: BTreeSet<String>,
    pub allowed_capabilities: BTreeSet<String>,
    pub region: String,
    pub maximum_concurrency: u32,
    pub maximum_requests: u64,
    pub maximum_input_units: u64,
    pub maximum_output_units: u64,
    pub not_before_unix_millis: u64,
    pub expires_unix_millis: u64,
    pub policy_epoch: u64,
    pub revocation_epoch: u64,
}
impl AdmissionGrantClaims {
    pub fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        if self.grant_id.kind() != "grant"
            || self.tenant_id.kind() != "tenant"
            || self.principal_id.is_empty()
            || self.principal_id.len() > 1024
            || self.region.is_empty()
            || self.region.len() > 128
            || self.maximum_concurrency == 0
            || self.maximum_requests == 0
            || self.not_before_unix_millis >= self.expires_unix_millis
            || self.policy_epoch == 0
            || self.revocation_epoch == 0
            || self.allowed_deployments.len() > MAX_SET_ENTRIES
            || self.allowed_capabilities.len() > MAX_CAPABILITIES
            || self
                .allowed_deployments
                .iter()
                .any(|value| value.is_empty() || value.len() > 256)
            || self
                .allowed_capabilities
                .iter()
                .any(|value| value.is_empty() || value.len() > 256)
        {
            return Err(Fault::invalid_argument(
                "admission grant claims are invalid or outside bounds",
            ));
        }
        let mut encoder = CanonicalEncoder::new("admission-grant-claims")?;
        encoder.text("grant_id", &self.grant_id.to_string())?;
        encoder.text("tenant_id", &self.tenant_id.to_string())?;
        encoder.text("principal_id", &self.principal_id)?;
        encoder.strings("allowed_deployments", self.allowed_deployments.iter())?;
        encoder.strings("allowed_capabilities", self.allowed_capabilities.iter())?;
        encoder.text("region", &self.region)?;
        encoder.u32("maximum_concurrency", self.maximum_concurrency)?;
        encoder.u64("maximum_requests", self.maximum_requests)?;
        encoder.u64("maximum_input_units", self.maximum_input_units)?;
        encoder.u64("maximum_output_units", self.maximum_output_units)?;
        encoder.u64("not_before_unix_millis", self.not_before_unix_millis)?;
        encoder.u64("expires_unix_millis", self.expires_unix_millis)?;
        encoder.u64("policy_epoch", self.policy_epoch)?;
        encoder.u64("revocation_epoch", self.revocation_epoch)?;
        Ok(encoder.finish())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdmissionGrant {
    pub claims: AdmissionGrantClaims,
    pub signature: DetachedSignature,
}
impl AdmissionGrant {
    pub fn validate<V: SignatureVerifier + ?Sized>(
        &self,
        now: u64,
        minimum_policy_epoch: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<()> {
        revocations.validate(now, self.claims.revocation_epoch, verifier)?;
        let bytes = self.claims.canonical_bytes()?;
        verifier.verify(&bytes, &self.signature)?;
        if now < self.claims.not_before_unix_millis || now >= self.claims.expires_unix_millis {
            return Err(Fault::new(
                Code::PermissionDenied,
                "admission grant is inactive",
            ));
        }
        if self.claims.policy_epoch < minimum_policy_epoch
            || self.claims.revocation_epoch < revocations.claims.epoch
            || revocations.grant_revoked(&self.claims.grant_id.to_string())
        {
            return Err(Fault::new(
                Code::PermissionDenied,
                "admission grant is stale or revoked",
            ));
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BufferAccess {
    ReadOnly,
    ReadWrite,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BufferTransport {
    SharedMemory,
    FileDescriptor,
    LocalFile,
    Artifact,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BufferDescriptor {
    pub segment_id: String,
    pub generation: u64,
    pub range: ByteRange,
    pub element_type: String,
    pub shape: Vec<u64>,
    pub digest: Digest,
    pub owner_process: String,
    pub lease_expires_unix_millis: u64,
    pub access: BufferAccess,
    pub transport: BufferTransport,
    pub locator: String,
}
impl BufferDescriptor {
    pub fn validate(&self, now: u64) -> FaultResult<()> {
        if self.segment_id.is_empty()
            || self.segment_id.len() > 256
            || self.owner_process.is_empty()
            || self.owner_process.len() > 256
            || self.locator.is_empty()
            || self.locator.len() > MAX_SHORT_TEXT_BYTES
            || self.element_type.is_empty()
            || self.element_type.len() > 128
            || self.generation == 0
            || self.shape.len() > MAX_INPUT_DIMENSIONS
            || self.shape.contains(&0)
        {
            return Err(Fault::invalid_argument(
                "buffer descriptor is incomplete or outside bounds",
            ));
        }
        if self.lease_expires_unix_millis <= now {
            return Err(Fault::new(Code::FailedPrecondition, "buffer lease expired"));
        }
        if self.range.is_empty() {
            return Err(Fault::invalid_argument("buffer descriptor is empty"));
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WorkerState {
    Created,
    Starting,
    Ready,
    Leased,
    Running,
    Draining,
    Committing,
    Completed,
    Recovering,
    Cancelling,
    Cancelled,
    Failed,
}

#[derive(Clone, Debug)]
pub enum WorkerCommand {
    Start {
        sequence: u64,
        ticket: ExecutionTicket,
        inputs: Vec<BufferDescriptor>,
        operation: String,
    },
    Cancel {
        sequence: u64,
        reason: String,
        deadline_unix_millis: u64,
    },
    Drain {
        sequence: u64,
        reason: String,
        deadline_unix_millis: u64,
    },
    Heartbeat {
        sequence: u64,
        requested_at_unix_millis: u64,
    },
}
impl WorkerCommand {
    #[must_use]
    pub fn sequence(&self) -> u64 {
        match self {
            Self::Start { sequence, .. }
            | Self::Cancel { sequence, .. }
            | Self::Drain { sequence, .. }
            | Self::Heartbeat { sequence, .. } => *sequence,
        }
    }
}

#[derive(Clone, Debug)]
pub struct WorkerStatus {
    pub sequence: u64,
    pub ticket_id: String,
    pub fencing_token: FencingToken,
    pub state: WorkerState,
    pub observed_unix_millis: u64,
    pub message: String,
    pub outputs: Vec<BufferDescriptor>,
    pub diagnostic_artifact: Option<Digest>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DeploymentRoute {
    pub deployment_id: ResourceId,
    pub model_bundle: Digest,
    pub engine_bundle: Digest,
    pub endpoint: String,
    pub region: String,
    pub weight: u32,
    pub capabilities: BTreeSet<String>,
    pub lease_expires_unix_millis: u64,
    pub safety_policy: Option<Digest>,
}
impl DeploymentRoute {
    fn validate(&self) -> FaultResult<()> {
        if self.deployment_id.kind() != "deployment"
            || self.endpoint.is_empty()
            || self.endpoint.len() > MAX_SHORT_TEXT_BYTES
            || self.region.is_empty()
            || self.region.len() > 128
            || self.weight == 0
            || self.capabilities.len() > MAX_CAPABILITIES
            || self
                .capabilities
                .iter()
                .any(|value| value.is_empty() || value.len() > 256)
        {
            return Err(Fault::invalid_argument(
                "deployment route is invalid or outside bounds",
            ));
        }
        Ok(())
    }
    fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        self.validate()?;
        let mut encoder = CanonicalEncoder::new("deployment-route")?;
        encoder.text("deployment_id", &self.deployment_id.to_string())?;
        encoder.text("model_bundle_digest", &self.model_bundle.to_string())?;
        encoder.text("engine_bundle_digest", &self.engine_bundle.to_string())?;
        encoder.text("endpoint", &self.endpoint)?;
        encoder.text("region", &self.region)?;
        encoder.u32("weight", self.weight)?;
        encoder.strings("capabilities", self.capabilities.iter())?;
        encoder.u64("lease_expires_unix_millis", self.lease_expires_unix_millis)?;
        encoder.text(
            "safety_policy_digest",
            &self
                .safety_policy
                .map(|value| value.to_string())
                .unwrap_or_default(),
        )?;
        Ok(encoder.finish())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RouteSnapshotClaims {
    pub snapshot_id: ResourceId,
    pub snapshot_digest: Digest,
    pub version: u64,
    pub policy_epoch: u64,
    pub revocation_epoch: u64,
    pub created_unix_millis: u64,
    pub expires_unix_millis: u64,
    pub routes: Vec<DeploymentRoute>,
    pub minimum_runtime_version: String,
}
impl RouteSnapshotClaims {
    fn canonical(&self, include_digest: bool) -> FaultResult<Vec<u8>> {
        if self.snapshot_id.kind() != "routesnap"
            || self.version == 0
            || self.policy_epoch == 0
            || self.revocation_epoch == 0
            || self.created_unix_millis >= self.expires_unix_millis
            || self.routes.is_empty()
            || self.routes.len() > MAX_ROUTES
            || self.minimum_runtime_version.is_empty()
            || self.minimum_runtime_version.len() > 128
        {
            return Err(Fault::invalid_argument(
                "route snapshot claims are invalid or outside bounds",
            ));
        }
        let mut previous: Option<String> = None;
        for route in &self.routes {
            route.validate()?;
            let id = route.deployment_id.to_string();
            if previous.as_ref().is_some_and(|previous| previous >= &id) {
                return Err(Fault::invalid_argument(
                    "deployment routes are not canonical",
                ));
            }
            previous = Some(id);
        }
        let mut encoder = CanonicalEncoder::new("route-snapshot-claims")?;
        encoder.text("snapshot_id", &self.snapshot_id.to_string())?;
        let digest_text = if include_digest {
            self.snapshot_digest.to_string()
        } else {
            String::new()
        };
        encoder.text("snapshot_digest", &digest_text)?;
        encoder.u64("version", self.version)?;
        encoder.u64("policy_epoch", self.policy_epoch)?;
        encoder.u64("revocation_epoch", self.revocation_epoch)?;
        encoder.u64("created_unix_millis", self.created_unix_millis)?;
        encoder.u64("expires_unix_millis", self.expires_unix_millis)?;
        let mut nested = Vec::new();
        for route in &self.routes {
            let route_bytes = route.canonical_bytes()?;
            let mut entry = CanonicalEncoder::new("route-list-entry")?;
            entry.nested("route", &route_bytes)?;
            let entry = entry.finish();
            let next = nested
                .len()
                .checked_add(entry.len())
                .ok_or_else(|| Fault::new(Code::OutOfRange, "route snapshot size overflow"))?;
            if next > MAX_CANONICAL_BYTES {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "route snapshot routes exceed canonical bound",
                ));
            }
            nested.extend_from_slice(&entry);
        }
        encoder.nested("routes", &nested)?;
        encoder.text("minimum_runtime_version", &self.minimum_runtime_version)?;
        Ok(encoder.finish())
    }
    pub fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        self.canonical(true)
    }
    pub fn computed_digest(&self) -> FaultResult<Digest> {
        Ok(hash_bytes(&self.canonical(false)?))
    }
    pub fn verify_digest(&self) -> FaultResult<()> {
        let expected = self.computed_digest()?;
        if expected == self.snapshot_digest {
            Ok(())
        } else {
            Err(Fault::data_loss("route snapshot digest mismatch"))
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RouteSnapshot {
    pub claims: RouteSnapshotClaims,
    pub signature: DetachedSignature,
}
impl RouteSnapshot {
    pub fn validate<V: SignatureVerifier + ?Sized>(
        &self,
        now: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<()> {
        revocations.validate(now, self.claims.revocation_epoch, verifier)?;
        self.claims.verify_digest()?;
        let bytes = self.claims.canonical_bytes()?;
        verifier.verify(&bytes, &self.signature)?;
        if now >= self.claims.expires_unix_millis {
            return Err(Fault::new(Code::DeadlineExceeded, "route snapshot expired"));
        }
        if self.claims.policy_epoch < minimum_policy_epoch
            || self.claims.version < minimum_route_version
            || self.claims.revocation_epoch < revocations.claims.epoch
        {
            return Err(Fault::new(
                Code::PermissionDenied,
                "route snapshot is stale",
            ));
        }
        // Individual deployment leases and revocations are evaluated during
        // request-specific route selection. A still-fresh signed snapshot may
        // therefore remain usable while one member deployment drains, expires,
        // or is revoked; rejecting the entire snapshot would make unrelated
        // healthy deployments unavailable.
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RevocationSnapshotClaims {
    pub epoch: u64,
    pub created_unix_millis: u64,
    pub expires_unix_millis: u64,
    pub revoked_grant_ids: BTreeSet<String>,
    pub revoked_ticket_ids: BTreeSet<String>,
    pub revoked_deployment_ids: BTreeSet<String>,
    pub revoked_bundle_digests: BTreeSet<String>,
}
impl RevocationSnapshotClaims {
    pub fn canonical_bytes(&self) -> FaultResult<Vec<u8>> {
        if self.epoch == 0
            || self.created_unix_millis >= self.expires_unix_millis
            || self.revoked_grant_ids.len() > MAX_REVOCATIONS_PER_CLASS
            || self.revoked_ticket_ids.len() > MAX_REVOCATIONS_PER_CLASS
            || self.revoked_deployment_ids.len() > MAX_REVOCATIONS_PER_CLASS
            || self.revoked_bundle_digests.len() > MAX_REVOCATIONS_PER_CLASS
            || self
                .revoked_grant_ids
                .iter()
                .chain(&self.revoked_ticket_ids)
                .chain(&self.revoked_deployment_ids)
                .any(|value| value.is_empty() || value.len() > 256)
            || self
                .revoked_bundle_digests
                .iter()
                .any(|value| value.is_empty() || value.len() > 128)
        {
            return Err(Fault::invalid_argument(
                "revocation snapshot claims are invalid or outside bounds",
            ));
        }
        let mut encoder = CanonicalEncoder::new("revocation-snapshot-claims")?;
        encoder.u64("epoch", self.epoch)?;
        encoder.u64("created_unix_millis", self.created_unix_millis)?;
        encoder.u64("expires_unix_millis", self.expires_unix_millis)?;
        encoder.strings("revoked_grant_ids", self.revoked_grant_ids.iter())?;
        encoder.strings("revoked_ticket_ids", self.revoked_ticket_ids.iter())?;
        encoder.strings("revoked_deployment_ids", self.revoked_deployment_ids.iter())?;
        encoder.strings("revoked_bundle_digests", self.revoked_bundle_digests.iter())?;
        Ok(encoder.finish())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RevocationSnapshot {
    pub claims: RevocationSnapshotClaims,
    pub signature: DetachedSignature,
}
impl RevocationSnapshot {
    /// Test-fixture constructor. The returned signature is intentionally not
    /// accepted by production verifiers; consumers must still fail closed.
    #[must_use]
    pub fn empty(epoch: u64, now: u64, expires: u64) -> Self {
        Self {
            claims: RevocationSnapshotClaims {
                epoch,
                created_unix_millis: now,
                expires_unix_millis: expires,
                revoked_grant_ids: BTreeSet::new(),
                revoked_ticket_ids: BTreeSet::new(),
                revoked_deployment_ids: BTreeSet::new(),
                revoked_bundle_digests: BTreeSet::new(),
            },
            signature: DetachedSignature {
                algorithm: "unverified-test-only".to_owned(),
                key_id: "none".to_owned(),
                value: vec![1],
            },
        }
    }
    pub fn validate<V: SignatureVerifier + ?Sized>(
        &self,
        now: u64,
        minimum_epoch: u64,
        verifier: &V,
    ) -> FaultResult<()> {
        let bytes = self.claims.canonical_bytes()?;
        verifier.verify(&bytes, &self.signature)?;
        if now >= self.claims.expires_unix_millis {
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "revocation snapshot expired",
            ));
        }
        if self.claims.epoch < minimum_epoch {
            return Err(Fault::new(
                Code::PermissionDenied,
                "revocation snapshot epoch is stale",
            ));
        }
        Ok(())
    }
    #[must_use]
    pub fn grant_revoked(&self, id: &str) -> bool {
        self.claims.revoked_grant_ids.contains(id)
    }
    #[must_use]
    pub fn ticket_revoked(&self, id: &str) -> bool {
        self.claims.revoked_ticket_ids.contains(id)
    }
    #[must_use]
    pub fn deployment_revoked(&self, id: &str) -> bool {
        self.claims.revoked_deployment_ids.contains(id)
    }
    #[must_use]
    pub fn bundle_revoked(&self, id: &str) -> bool {
        self.claims.revoked_bundle_digests.contains(id)
    }
}
