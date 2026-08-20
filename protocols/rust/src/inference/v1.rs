// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Protobuf-compatible inference v1 messages and coarse admission.
//!
//! ADR-0016 splits inference batching in two. The runtime data plane admits and
//! coarsely groups requests using classes the model itself declared; Python
//! decides which admitted requests may actually share tensors. This module is
//! the Rust half of that split, so the matching rules live beside the wire
//! projection instead of being re-derived by each service.
//!
//! Nothing here interprets model internals. A rejection means the request may
//! not enter a class, never that a particular tensor layout is impossible.

/// Upper bound on declared capability strings, matching the runtime worker
/// protocol and the Python request contract.
pub const MAX_CAPABILITIES: usize = 256;
/// Upper bound on declared compatibility classes for one model.
pub const MAX_COMPATIBILITY_CLASSES: usize = 64;
/// Upper bound on any single free-text field in a descriptor.
pub const MAX_TEXT_BYTES: usize = 4096;
/// Byte length of the canonical `sha256:<64 hex>` digest text form.
pub const DIGEST_TEXT_BYTES: usize = 71;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord, ::prost::Enumeration)]
#[repr(i32)]
pub enum ModelPrecision {
    Unspecified = 0,
    Fp32 = 1,
    Tf32 = 2,
    Bf16 = 3,
    Fp16 = 4,
    Fp8 = 5,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord, ::prost::Enumeration)]
#[repr(i32)]
pub enum ExecutionKind {
    Unspecified = 0,
    Forward = 1,
    DiffusionSample = 2,
    Embedding = 3,
    Scoring = 4,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord, ::prost::Enumeration)]
#[repr(i32)]
pub enum ModelLifecycle {
    Unspecified = 0,
    Draft = 1,
    Qualified = 2,
    Serving = 3,
    Deprecated = 4,
    Revoked = 5,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CompatibilityClass {
    #[prost(string, tag = "1")]
    pub class_id: String,
    #[prost(enumeration = "ExecutionKind", tag = "2")]
    pub execution_kind: i32,
    #[prost(enumeration = "ModelPrecision", tag = "3")]
    pub precision: i32,
    #[prost(string, tag = "4")]
    pub shape_bucket: String,
    #[prost(uint32, tag = "5")]
    pub maximum_batch_requests: u32,
    #[prost(uint64, tag = "6")]
    pub maximum_batch_gpu_bytes: u64,
    #[prost(uint64, tag = "7")]
    pub maximum_input_units: u64,
    #[prost(uint64, tag = "8")]
    pub maximum_output_units: u64,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ModelResourceEnvelope {
    #[prost(uint64, tag = "1")]
    pub weights_resident_bytes: u64,
    #[prost(uint64, tag = "2")]
    pub host_memory_bytes: u64,
    #[prost(uint64, tag = "3")]
    pub gpu_memory_floor_bytes: u64,
    #[prost(uint64, tag = "4")]
    pub gpu_memory_per_request_bytes: u64,
    #[prost(uint32, tag = "5")]
    pub maximum_concurrent_requests: u32,
    #[prost(uint32, tag = "6")]
    pub load_deadline_millis: u32,
    #[prost(uint32, tag = "7")]
    pub drain_deadline_millis: u32,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ModelDescriptor {
    #[prost(string, tag = "1")]
    pub descriptor_digest: String,
    #[prost(string, tag = "2")]
    pub model_id: String,
    #[prost(string, tag = "3")]
    pub family: String,
    #[prost(string, tag = "4")]
    pub version: String,
    #[prost(enumeration = "ModelLifecycle", tag = "5")]
    pub lifecycle: i32,
    #[prost(string, tag = "6")]
    pub model_bundle_digest: String,
    #[prost(string, tag = "7")]
    pub engine_bundle_digest: String,
    #[prost(string, tag = "8")]
    pub resolved_config_digest: String,
    #[prost(string, tag = "9")]
    pub kernel_manifest_digest: String,
    #[prost(string, tag = "10")]
    pub safety_policy_digest: String,
    #[prost(string, repeated, tag = "11")]
    pub capabilities: Vec<String>,
    #[prost(message, repeated, tag = "12")]
    pub compatibility_classes: Vec<CompatibilityClass>,
    #[prost(message, optional, tag = "13")]
    pub envelope: Option<ModelResourceEnvelope>,
    #[prost(string, tag = "14")]
    pub accelerator_capability: String,
    #[prost(string, tag = "15")]
    pub minimum_runtime_version: String,
    #[prost(uint32, tag = "16")]
    pub schema_version: u32,
    #[prost(uint64, tag = "17")]
    pub policy_epoch: u64,
    #[prost(uint64, tag = "18")]
    pub created_unix_millis: u64,
    #[prost(uint64, tag = "19")]
    pub expires_unix_millis: u64,
}

/// Why a descriptor is unusable, or why a request may not enter any class.
///
/// The variants stay coarse on purpose: they are safe to return to a caller
/// across a trust boundary and none of them disclose model internals.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ModelContractError {
    DescriptorDigestInvalid,
    IdentityInvalid,
    BundleDigestInvalid,
    LifecycleNotServable,
    CapabilitiesNotCanonical,
    CapabilityCountExceeded,
    CompatibilityClassesInvalid,
    EnvelopeInvalid,
    SchemaVersionInvalid,
    ValidityWindowInvalid,
    DescriptorExpired,
    CapabilityUnsupported,
    NoCompatibleClass,
    RequestUnitsExceeded,
}

impl ModelContractError {
    /// Stable, non-disclosing message for logs and transport statuses.
    #[must_use]
    pub const fn message(self) -> &'static str {
        match self {
            Self::DescriptorDigestInvalid => "model descriptor digest is not canonical",
            Self::IdentityInvalid => "model descriptor identity is invalid",
            Self::BundleDigestInvalid => "model or engine bundle digest is not canonical",
            Self::LifecycleNotServable => "model lifecycle does not permit serving",
            Self::CapabilitiesNotCanonical => "model capabilities must be sorted and unique",
            Self::CapabilityCountExceeded => "model capability count exceeds bound",
            Self::CompatibilityClassesInvalid => "model compatibility classes are invalid",
            Self::EnvelopeInvalid => "model resource envelope is invalid",
            Self::SchemaVersionInvalid => "model descriptor schema version is invalid",
            Self::ValidityWindowInvalid => "model descriptor validity window is invalid",
            Self::DescriptorExpired => "model descriptor has expired",
            Self::CapabilityUnsupported => "model does not declare a required capability",
            Self::NoCompatibleClass => "no declared compatibility class admits the request",
            Self::RequestUnitsExceeded => "request units exceed the compatibility class bound",
        }
    }
}

impl core::fmt::Display for ModelContractError {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter.write_str(self.message())
    }
}

impl core::error::Error for ModelContractError {}

impl From<ModelContractError> for tonic::Status {
    fn from(error: ModelContractError) -> Self {
        Self::failed_precondition(error.message())
    }
}

/// The coarse facts the data plane knows about a request before Python sees it.
#[derive(Clone, Copy, Debug)]
pub struct AdmissionRequest<'a> {
    pub execution_kind: ExecutionKind,
    pub precision: ModelPrecision,
    pub shape_bucket: &'a str,
    pub required_capabilities: &'a [String],
    pub input_units: u64,
    pub output_units: u64,
    pub now_unix_millis: u64,
}

fn canonical_digest(value: &str) -> bool {
    value.len() == DIGEST_TEXT_BYTES
        && value.starts_with("sha256:")
        && value[7..]
            .bytes()
            .all(|byte| byte.is_ascii_hexdigit() && !byte.is_ascii_uppercase())
}

fn bounded_text(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= MAX_TEXT_BYTES
        && !value.contains('\n')
        && !value.contains('|')
}

fn sorted_unique(values: &[String]) -> bool {
    values.windows(2).all(|pair| pair[0] < pair[1])
}

impl CompatibilityClass {
    /// Validate one declared class.
    ///
    /// # Errors
    ///
    /// Returns [`ModelContractError::CompatibilityClassesInvalid`] when the
    /// class identity, enums, or batch bounds are not usable for reservation.
    pub fn validate(&self) -> Result<(), ModelContractError> {
        let kind = ExecutionKind::try_from(self.execution_kind);
        let precision = ModelPrecision::try_from(self.precision);
        let usable = bounded_text(&self.class_id)
            && bounded_text(&self.shape_bucket)
            && matches!(kind, Ok(value) if value != ExecutionKind::Unspecified)
            && matches!(precision, Ok(value) if value != ModelPrecision::Unspecified)
            && self.maximum_batch_requests > 0
            && self.maximum_batch_gpu_bytes > 0
            && self.maximum_input_units > 0
            && self.maximum_output_units > 0;
        if usable {
            Ok(())
        } else {
            Err(ModelContractError::CompatibilityClassesInvalid)
        }
    }

    /// Whether this class matches the coarse shape of a request.
    #[must_use]
    pub fn matches(&self, request: &AdmissionRequest<'_>) -> bool {
        self.execution_kind == request.execution_kind as i32
            && self.precision == request.precision as i32
            && self.shape_bucket == request.shape_bucket
    }
}

impl ModelResourceEnvelope {
    /// Validate the declared reservation envelope.
    ///
    /// # Errors
    ///
    /// Returns [`ModelContractError::EnvelopeInvalid`] when a floor, increment,
    /// concurrency bound, or lifecycle deadline is zero.
    pub fn validate(&self) -> Result<(), ModelContractError> {
        let usable = self.weights_resident_bytes > 0
            && self.host_memory_bytes > 0
            && self.gpu_memory_floor_bytes > 0
            && self.gpu_memory_per_request_bytes > 0
            && self.maximum_concurrent_requests > 0
            && self.load_deadline_millis > 0
            && self.drain_deadline_millis > 0;
        if usable {
            Ok(())
        } else {
            Err(ModelContractError::EnvelopeInvalid)
        }
    }

    /// GPU bytes to reserve for `concurrency` simultaneous requests.
    ///
    /// # Errors
    ///
    /// Returns [`ModelContractError::EnvelopeInvalid`] when `concurrency`
    /// exceeds the declared bound or the reservation would overflow `u64`.
    pub fn gpu_reservation_bytes(&self, concurrency: u32) -> Result<u64, ModelContractError> {
        if concurrency == 0 || concurrency > self.maximum_concurrent_requests {
            return Err(ModelContractError::EnvelopeInvalid);
        }
        self.gpu_memory_per_request_bytes
            .checked_mul(u64::from(concurrency))
            .and_then(|scaled| scaled.checked_add(self.gpu_memory_floor_bytes))
            .ok_or(ModelContractError::EnvelopeInvalid)
    }
}

impl ModelDescriptor {
    /// Validate the descriptor as published catalog state.
    ///
    /// This checks internal consistency and declared bounds only. It does not
    /// recompute `descriptor_digest`: the control plane seals that value, and
    /// the data plane trusts the signed snapshot that carried the descriptor.
    ///
    /// # Errors
    ///
    /// Returns the [`ModelContractError`] naming the first failed invariant.
    pub fn validate(&self) -> Result<(), ModelContractError> {
        if !canonical_digest(&self.descriptor_digest) {
            return Err(ModelContractError::DescriptorDigestInvalid);
        }
        if !bounded_text(&self.model_id)
            || !bounded_text(&self.family)
            || !bounded_text(&self.version)
            || !bounded_text(&self.accelerator_capability)
            || !bounded_text(&self.minimum_runtime_version)
        {
            return Err(ModelContractError::IdentityInvalid);
        }
        if !canonical_digest(&self.model_bundle_digest)
            || !canonical_digest(&self.engine_bundle_digest)
            || !canonical_digest(&self.resolved_config_digest)
            || !canonical_digest(&self.kernel_manifest_digest)
            || !canonical_digest(&self.safety_policy_digest)
        {
            return Err(ModelContractError::BundleDigestInvalid);
        }
        if self.schema_version == 0 {
            return Err(ModelContractError::SchemaVersionInvalid);
        }
        if self.capabilities.len() > MAX_CAPABILITIES {
            return Err(ModelContractError::CapabilityCountExceeded);
        }
        if !sorted_unique(&self.capabilities)
            || self.capabilities.iter().any(|value| !bounded_text(value))
        {
            return Err(ModelContractError::CapabilitiesNotCanonical);
        }
        if self.compatibility_classes.is_empty()
            || self.compatibility_classes.len() > MAX_COMPATIBILITY_CLASSES
        {
            return Err(ModelContractError::CompatibilityClassesInvalid);
        }
        for (index, class) in self.compatibility_classes.iter().enumerate() {
            class.validate()?;
            if self.compatibility_classes[..index]
                .iter()
                .any(|earlier| earlier.class_id == class.class_id)
            {
                return Err(ModelContractError::CompatibilityClassesInvalid);
            }
        }
        let envelope = self
            .envelope
            .as_ref()
            .ok_or(ModelContractError::EnvelopeInvalid)?;
        envelope.validate()?;
        if self.created_unix_millis == 0 || self.expires_unix_millis <= self.created_unix_millis {
            return Err(ModelContractError::ValidityWindowInvalid);
        }
        Ok(())
    }

    /// Whether the declared lifecycle permits serving traffic.
    #[must_use]
    pub fn servable(&self) -> bool {
        self.lifecycle == ModelLifecycle::Serving as i32
    }

    /// Select the compatibility class that admits `request`.
    ///
    /// This is the coarse half of ADR-0016. A returned class means the request
    /// may enter that admission group and be reserved against the envelope; it
    /// does not promise the Python worker will batch it with any other request.
    ///
    /// # Errors
    ///
    /// Returns a [`ModelContractError`] when the descriptor is unusable, has
    /// expired, lacks a required capability, declares no matching class, or
    /// when the request exceeds the matched class's unit bounds.
    pub fn admit(
        &self,
        request: &AdmissionRequest<'_>,
    ) -> Result<&CompatibilityClass, ModelContractError> {
        self.validate()?;
        if !self.servable() {
            return Err(ModelContractError::LifecycleNotServable);
        }
        if request.now_unix_millis >= self.expires_unix_millis {
            return Err(ModelContractError::DescriptorExpired);
        }
        for required in request.required_capabilities {
            if self.capabilities.binary_search(required).is_err() {
                return Err(ModelContractError::CapabilityUnsupported);
            }
        }
        let class = self
            .compatibility_classes
            .iter()
            .find(|class| class.matches(request))
            .ok_or(ModelContractError::NoCompatibleClass)?;
        if request.input_units == 0
            || request.output_units == 0
            || request.input_units > class.maximum_input_units
            || request.output_units > class.maximum_output_units
        {
            return Err(ModelContractError::RequestUnitsExceeded);
        }
        Ok(class)
    }
}
