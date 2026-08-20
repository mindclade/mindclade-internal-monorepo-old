# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import dataclasses

import pytest

from serving.contracts import (
    BatchPlan,
    CompatibilityClass,
    CompatibilityKey,
    InferenceRequest,
    InputDescriptor,
    ModelBundle,
    ModelDescriptor,
    ResourceEnvelope,
    validate_batch_against_descriptor,
    validate_bundle_against_descriptor,
)

CREATED = 1_800_000_000_000
EXPIRES = CREATED + 86_400_000
NOW = CREATED + 100_000


def digest(fill: str) -> str:
    return "sha256:" + fill * 64


def forward_class() -> CompatibilityClass:
    return CompatibilityClass(
        class_id="forward-bf16-small",
        execution_kind="forward",
        precision="bf16",
        shape_bucket="tokens<=1024",
        maximum_batch_requests=8,
        maximum_batch_gpu_bytes=8 << 30,
        maximum_input_units=1024,
        maximum_output_units=512,
    )


def diffusion_class() -> CompatibilityClass:
    return CompatibilityClass(
        class_id="diffusion-fp16-large",
        execution_kind="diffusion_sample",
        precision="fp16",
        shape_bucket="atoms<=8192",
        maximum_batch_requests=2,
        maximum_batch_gpu_bytes=32 << 30,
        maximum_input_units=8192,
        maximum_output_units=4096,
    )


def unsealed_descriptor() -> ModelDescriptor:
    return ModelDescriptor(
        descriptor_digest="",
        model_id="model_019c0000000070008000000000000001",
        family="novafold",
        version="3.1.0",
        lifecycle="serving",
        model_bundle_digest=digest("1"),
        engine_bundle_digest=digest("2"),
        resolved_config_digest=digest("3"),
        kernel_manifest_digest=digest("4"),
        safety_policy_digest=digest("5"),
        capabilities=("msa", "structure", "templates"),
        compatibility_classes=(forward_class(), diffusion_class()),
        envelope=ResourceEnvelope(
            weights_resident_bytes=24 << 30,
            host_memory_bytes=32 << 30,
            gpu_memory_floor_bytes=40 << 30,
            gpu_memory_per_request_bytes=2 << 30,
            maximum_concurrent_requests=4,
            load_deadline_millis=120_000,
            drain_deadline_millis=30_000,
        ),
        accelerator_capability="sm90",
        minimum_runtime_version="1.4.0",
        schema_version=1,
        policy_epoch=12,
        created_unix_millis=CREATED,
        expires_unix_millis=EXPIRES,
    )


def sealed_descriptor() -> ModelDescriptor:
    draft = unsealed_descriptor()
    return dataclasses.replace(draft, descriptor_digest=draft.sealed_digest)


def batch_for(descriptor: ModelDescriptor, *, requests: int = 1) -> BatchPlan:
    input_descriptor = InputDescriptor("segment", digest("a"), "/tmp/segment", 4, 1, EXPIRES)
    inference_requests = tuple(
        InferenceRequest(
            f"request-{index}",
            descriptor.model_bundle_digest,
            b"key",
            (input_descriptor,),
            ("structure",),
            4,
            4,
            EXPIRES,
        )
        for index in range(requests)
    )
    return BatchPlan(
        CompatibilityKey(descriptor.model_bundle_digest, "forward", "bf16", "tokens<=1024"),
        inference_requests,
        1 << 30,
        "small-bf16",
    )


def test_sealing_is_stable_and_order_independent() -> None:
    descriptor = sealed_descriptor()
    descriptor.verify_digest()
    reordered = dataclasses.replace(
        descriptor,
        compatibility_classes=(diffusion_class(), forward_class()),
    )
    assert reordered.canonical_bytes() == descriptor.canonical_bytes()
    assert descriptor.canonical_bytes().startswith(b"inference-model-descriptor/v1\n")


def test_verify_digest_rejects_mutated_content() -> None:
    tampered = dataclasses.replace(sealed_descriptor(), version="3.1.1")
    with pytest.raises(ValueError, match="does not match its content"):
        tampered.verify_digest()


def test_verify_digest_rejects_an_unsealed_descriptor() -> None:
    with pytest.raises(ValueError, match="not canonical"):
        unsealed_descriptor().verify_digest()


def test_validation_rejects_reserved_delimiters_in_capabilities() -> None:
    smuggled = dataclasses.replace(unsealed_descriptor(), capabilities=("structure|forged",))
    with pytest.raises(ValueError, match="reserved delimiter"):
        smuggled.validate()


def test_validation_rejects_unsorted_capabilities() -> None:
    unsorted = dataclasses.replace(unsealed_descriptor(), capabilities=("structure", "msa"))
    with pytest.raises(ValueError, match="sorted and unique"):
        unsorted.validate()


def test_bundle_must_match_the_descriptor() -> None:
    descriptor = sealed_descriptor()
    bundle = ModelBundle(
        model_digest=descriptor.model_bundle_digest,
        runtime_digest=descriptor.engine_bundle_digest,
        capabilities=("msa", "structure"),
    )
    validate_bundle_against_descriptor(bundle, descriptor)

    mismatched = dataclasses.replace(bundle, model_digest=digest("9"))
    with pytest.raises(ValueError, match="does not match the descriptor"):
        validate_bundle_against_descriptor(mismatched, descriptor)

    overreaching = dataclasses.replace(bundle, capabilities=("ligands", "structure"))
    with pytest.raises(ValueError, match="does not declare"):
        validate_bundle_against_descriptor(overreaching, descriptor)


def test_batch_matches_a_declared_class() -> None:
    descriptor = sealed_descriptor()
    matched = validate_batch_against_descriptor(
        batch_for(descriptor), descriptor, now_unix_millis=NOW
    )
    assert matched.class_id == "forward-bf16-small"


def test_batch_beyond_the_class_request_bound_is_rejected() -> None:
    descriptor = sealed_descriptor()
    with pytest.raises(ValueError, match="request bound"):
        validate_batch_against_descriptor(
            batch_for(descriptor, requests=9), descriptor, now_unix_millis=NOW
        )


def test_batch_against_an_undeclared_class_is_rejected() -> None:
    descriptor = sealed_descriptor()
    batch = batch_for(descriptor)
    mismatched = dataclasses.replace(
        batch,
        compatibility=CompatibilityKey(
            descriptor.model_bundle_digest, "forward", "fp8", "tokens<=1024"
        ),
    )
    with pytest.raises(ValueError, match="any declared compatibility class"):
        validate_batch_against_descriptor(mismatched, descriptor, now_unix_millis=NOW)


def test_expired_or_retired_descriptor_is_not_servable() -> None:
    descriptor = sealed_descriptor()
    assert descriptor.servable(NOW)
    assert not descriptor.servable(EXPIRES)
    with pytest.raises(ValueError, match="not servable"):
        validate_batch_against_descriptor(
            batch_for(descriptor), descriptor, now_unix_millis=EXPIRES
        )
