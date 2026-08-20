// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_protocols::inference::v1::{
    AdmissionRequest, CompatibilityClass, ExecutionKind, ModelContractError, ModelDescriptor,
    ModelLifecycle, ModelPrecision, ModelResourceEnvelope,
};
use prost::Message;

fn digest(fill: char) -> String {
    let mut value = String::from("sha256:");
    for _ in 0..64 {
        value.push(fill);
    }
    value
}

fn forward_class() -> CompatibilityClass {
    CompatibilityClass {
        class_id: "forward-bf16-small".into(),
        execution_kind: ExecutionKind::Forward as i32,
        precision: ModelPrecision::Bf16 as i32,
        shape_bucket: "tokens<=1024".into(),
        maximum_batch_requests: 8,
        maximum_batch_gpu_bytes: 8 << 30,
        maximum_input_units: 1024,
        maximum_output_units: 512,
    }
}

fn diffusion_class() -> CompatibilityClass {
    CompatibilityClass {
        class_id: "diffusion-fp16-large".into(),
        execution_kind: ExecutionKind::DiffusionSample as i32,
        precision: ModelPrecision::Fp16 as i32,
        shape_bucket: "atoms<=8192".into(),
        maximum_batch_requests: 2,
        maximum_batch_gpu_bytes: 32 << 30,
        maximum_input_units: 8192,
        maximum_output_units: 4096,
    }
}

fn descriptor() -> ModelDescriptor {
    ModelDescriptor {
        descriptor_digest: digest('0'),
        model_id: "model_019c0000000070008000000000000001".into(),
        family: "novafold".into(),
        version: "3.1.0".into(),
        lifecycle: ModelLifecycle::Serving as i32,
        model_bundle_digest: digest('1'),
        engine_bundle_digest: digest('2'),
        resolved_config_digest: digest('3'),
        kernel_manifest_digest: digest('4'),
        safety_policy_digest: digest('5'),
        capabilities: vec!["msa".into(), "structure".into(), "templates".into()],
        compatibility_classes: vec![forward_class(), diffusion_class()],
        envelope: Some(ModelResourceEnvelope {
            weights_resident_bytes: 24 << 30,
            host_memory_bytes: 32 << 30,
            gpu_memory_floor_bytes: 40 << 30,
            gpu_memory_per_request_bytes: 2 << 30,
            maximum_concurrent_requests: 4,
            load_deadline_millis: 120_000,
            drain_deadline_millis: 30_000,
        }),
        accelerator_capability: "sm90".into(),
        minimum_runtime_version: "1.4.0".into(),
        schema_version: 1,
        policy_epoch: 12,
        created_unix_millis: 1_800_000_000_000,
        expires_unix_millis: 1_800_000_600_000,
    }
}

fn request<'a>(capabilities: &'a [String], bucket: &'a str) -> AdmissionRequest<'a> {
    AdmissionRequest {
        execution_kind: ExecutionKind::Forward,
        precision: ModelPrecision::Bf16,
        shape_bucket: bucket,
        required_capabilities: capabilities,
        input_units: 512,
        output_units: 256,
        now_unix_millis: 1_800_000_100_000,
    }
}

#[test]
fn model_descriptor_round_trips_through_protobuf() {
    let original = descriptor();
    let encoded = original.encode_to_vec();
    let decoded = ModelDescriptor::decode(encoded.as_slice()).expect("decode");
    assert_eq!(decoded, original);
}

#[test]
fn published_descriptor_validates() {
    descriptor().validate().expect("descriptor is valid");
}

#[test]
fn admission_selects_the_declared_class() {
    let capabilities = vec!["msa".to_owned(), "structure".to_owned()];
    let descriptor = descriptor();
    let admitted = descriptor
        .admit(&request(&capabilities, "tokens<=1024"))
        .expect("request is admitted");
    assert_eq!(admitted.class_id, "forward-bf16-small");
}

#[test]
fn admission_rejects_an_undeclared_capability() {
    let capabilities = vec!["ligands".to_owned()];
    let error = descriptor()
        .admit(&request(&capabilities, "tokens<=1024"))
        .expect_err("undeclared capability is rejected");
    assert_eq!(error, ModelContractError::CapabilityUnsupported);
}

#[test]
fn admission_rejects_an_unmatched_shape_bucket() {
    let capabilities: Vec<String> = Vec::new();
    let error = descriptor()
        .admit(&request(&capabilities, "tokens<=4096"))
        .expect_err("unmatched shape bucket is rejected");
    assert_eq!(error, ModelContractError::NoCompatibleClass);
}

#[test]
fn admission_rejects_units_beyond_the_class_bound() {
    let capabilities: Vec<String> = Vec::new();
    let mut oversized = request(&capabilities, "tokens<=1024");
    oversized.input_units = 4096;
    let error = descriptor()
        .admit(&oversized)
        .expect_err("oversized request is rejected");
    assert_eq!(error, ModelContractError::RequestUnitsExceeded);
}

#[test]
fn admission_rejects_an_expired_descriptor() {
    let capabilities: Vec<String> = Vec::new();
    let mut late = request(&capabilities, "tokens<=1024");
    late.now_unix_millis = 1_800_000_600_000;
    let error = descriptor()
        .admit(&late)
        .expect_err("expired descriptor is rejected");
    assert_eq!(error, ModelContractError::DescriptorExpired);
}

#[test]
fn admission_rejects_a_model_that_is_not_serving() {
    let capabilities: Vec<String> = Vec::new();
    let mut deprecated = descriptor();
    deprecated.lifecycle = ModelLifecycle::Deprecated as i32;
    let error = deprecated
        .admit(&request(&capabilities, "tokens<=1024"))
        .expect_err("non-serving lifecycle is rejected");
    assert_eq!(error, ModelContractError::LifecycleNotServable);
}

#[test]
fn validation_rejects_unsorted_capabilities() {
    let mut unsorted = descriptor();
    unsorted.capabilities = vec!["structure".into(), "msa".into()];
    assert_eq!(
        unsorted.validate().expect_err("unsorted capabilities"),
        ModelContractError::CapabilitiesNotCanonical
    );
}

#[test]
fn validation_rejects_duplicate_class_identifiers() {
    let mut duplicated = descriptor();
    duplicated.compatibility_classes = vec![forward_class(), forward_class()];
    assert_eq!(
        duplicated.validate().expect_err("duplicate class ids"),
        ModelContractError::CompatibilityClassesInvalid
    );
}

#[test]
fn validation_rejects_a_noncanonical_digest() {
    let mut broken = descriptor();
    broken.model_bundle_digest = "sha256:NOTHEX".into();
    assert_eq!(
        broken.validate().expect_err("noncanonical digest"),
        ModelContractError::BundleDigestInvalid
    );
}

#[test]
fn gpu_reservation_scales_with_concurrency_and_is_bounded() {
    let envelope = descriptor().envelope.expect("envelope");
    let reserved = envelope.gpu_reservation_bytes(2).expect("reservation");
    assert_eq!(reserved, (40 << 30) + (2 * (2_u64 << 30)));
    assert_eq!(
        envelope
            .gpu_reservation_bytes(5)
            .expect_err("beyond declared concurrency"),
        ModelContractError::EnvelopeInvalid
    );
}
