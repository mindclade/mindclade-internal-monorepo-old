# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Worker-side projection of ``mindclade.inference.v1.ModelDescriptor``.

The Go control plane is the only writer of a descriptor: it validates one,
seals ``descriptor_digest`` over the canonical encoding, and publishes it. This
module is the verifier at the other end. A worker recomputes the digest of the
descriptor it was handed and refuses to serve when the seal does not match, so a
descriptor mutated in transit or in a cache cannot reach model execution.

Python stays authoritative for final tensor batching. Nothing here decides which
requests share tensors; it checks that the batch a planner produced belongs to a
class the model actually declared, and that the model is servable at all.
"""

from __future__ import annotations

import hashlib
from dataclasses import dataclass
from typing import Final

from .batch import BatchPlan
from .model_bundle import ModelBundle

CANONICAL_DOCUMENT_TYPE: Final = "inference-model-descriptor/v1"
MAX_CAPABILITIES: Final = 256
MAX_COMPATIBILITY_CLASSES: Final = 64
MAX_TEXT_BYTES: Final = 4096
DIGEST_TEXT_LENGTH: Final = 71

LIFECYCLES: Final = frozenset({"draft", "qualified", "serving", "deprecated", "revoked"})
EXECUTION_KINDS: Final = frozenset({"forward", "diffusion_sample", "embedding", "scoring"})
PRECISIONS: Final = frozenset({"fp32", "tf32", "bf16", "fp16", "fp8"})


def _canonical_digest(value: str) -> bool:
    if len(value) != DIGEST_TEXT_LENGTH or not value.startswith("sha256:"):
        return False
    return all(character in "0123456789abcdef" for character in value[7:])


def _canonical_text(value: str) -> bool:
    """Reject the two delimiters the canonical encoding reserves.

    Without this the encoding is not injective: a capability containing a
    vertical bar could impersonate a compatibility-class line and two different
    descriptors would seal to the same digest.
    """
    return bool(value) and len(value.encode()) <= MAX_TEXT_BYTES and not set(value) & {"\n", "|"}


@dataclass(frozen=True, slots=True)
class CompatibilityClass:
    class_id: str
    execution_kind: str
    precision: str
    shape_bucket: str
    maximum_batch_requests: int
    maximum_batch_gpu_bytes: int
    maximum_input_units: int
    maximum_output_units: int

    def validate(self) -> None:
        if not _canonical_text(self.class_id) or not _canonical_text(self.shape_bucket):
            raise ValueError("compatibility class id and shape bucket are invalid")
        if self.execution_kind not in EXECUTION_KINDS:
            raise ValueError("compatibility class execution kind is not declared")
        if self.precision not in PRECISIONS:
            raise ValueError("compatibility class precision is not declared")
        if self.maximum_batch_requests <= 0 or self.maximum_batch_gpu_bytes <= 0:
            raise ValueError("compatibility class batch bounds must be positive")
        if self.maximum_input_units <= 0 or self.maximum_output_units <= 0:
            raise ValueError("compatibility class unit bounds must be positive")

    def canonical_line(self) -> str:
        return "|".join(
            (
                "class",
                self.class_id,
                self.execution_kind,
                self.precision,
                self.shape_bucket,
                str(self.maximum_batch_requests),
                str(self.maximum_batch_gpu_bytes),
                str(self.maximum_input_units),
                str(self.maximum_output_units),
            )
        )


@dataclass(frozen=True, slots=True)
class ResourceEnvelope:
    weights_resident_bytes: int
    host_memory_bytes: int
    gpu_memory_floor_bytes: int
    gpu_memory_per_request_bytes: int
    maximum_concurrent_requests: int
    load_deadline_millis: int
    drain_deadline_millis: int

    def validate(self) -> None:
        values = (
            self.weights_resident_bytes,
            self.host_memory_bytes,
            self.gpu_memory_floor_bytes,
            self.gpu_memory_per_request_bytes,
            self.maximum_concurrent_requests,
            self.load_deadline_millis,
            self.drain_deadline_millis,
        )
        if any(value <= 0 for value in values):
            raise ValueError("model resource envelope fields must be positive")

    def canonical_line(self) -> str:
        return "|".join(
            (
                "envelope",
                str(self.weights_resident_bytes),
                str(self.host_memory_bytes),
                str(self.gpu_memory_floor_bytes),
                str(self.gpu_memory_per_request_bytes),
                str(self.maximum_concurrent_requests),
                str(self.load_deadline_millis),
                str(self.drain_deadline_millis),
            )
        )


@dataclass(frozen=True, slots=True)
class ModelDescriptor:
    descriptor_digest: str
    model_id: str
    family: str
    version: str
    lifecycle: str
    model_bundle_digest: str
    engine_bundle_digest: str
    resolved_config_digest: str
    kernel_manifest_digest: str
    safety_policy_digest: str
    capabilities: tuple[str, ...]
    compatibility_classes: tuple[CompatibilityClass, ...]
    envelope: ResourceEnvelope
    accelerator_capability: str
    minimum_runtime_version: str
    schema_version: int
    policy_epoch: int
    created_unix_millis: int
    expires_unix_millis: int

    def validate(self) -> None:
        for value in (
            self.model_id,
            self.family,
            self.version,
            self.accelerator_capability,
            self.minimum_runtime_version,
        ):
            if not _canonical_text(value):
                raise ValueError("model descriptor identity is invalid")
        if self.lifecycle not in LIFECYCLES:
            raise ValueError("model lifecycle is not a declared state")
        for digest in (
            self.model_bundle_digest,
            self.engine_bundle_digest,
            self.resolved_config_digest,
            self.kernel_manifest_digest,
            self.safety_policy_digest,
        ):
            if not _canonical_digest(digest):
                raise ValueError("model descriptor bundle digest is not canonical")
        if self.schema_version <= 0 or self.policy_epoch <= 0:
            raise ValueError("model descriptor schema version and policy epoch must be positive")
        if len(self.capabilities) > MAX_CAPABILITIES:
            raise ValueError("model capability count exceeds bound")
        if any(not _canonical_text(capability) for capability in self.capabilities):
            raise ValueError("model capability is empty or contains a reserved delimiter")
        if tuple(sorted(set(self.capabilities))) != self.capabilities:
            raise ValueError("model capabilities must be sorted and unique")
        if not 0 < len(self.compatibility_classes) <= MAX_COMPATIBILITY_CLASSES:
            raise ValueError("model must declare between one and 64 compatibility classes")
        class_ids = [entry.class_id for entry in self.compatibility_classes]
        if len(class_ids) != len(set(class_ids)):
            raise ValueError("compatibility class ids must be unique")
        for entry in self.compatibility_classes:
            entry.validate()
        self.envelope.validate()
        if self.created_unix_millis <= 0 or self.expires_unix_millis <= self.created_unix_millis:
            raise ValueError("model descriptor must expire after it was created")

    def canonical_bytes(self) -> bytes:
        """Reproduce the Go writer's ``inference-model-descriptor/v1`` document.

        Both sides emit repeated fields in sorted order so the encoding does not
        depend on the order a descriptor happened to be assembled in.
        """
        self.validate()
        lines = [
            CANONICAL_DOCUMENT_TYPE,
            self.model_id,
            self.family,
            self.version,
            self.lifecycle,
            self.model_bundle_digest,
            self.engine_bundle_digest,
            self.resolved_config_digest,
            self.kernel_manifest_digest,
            self.safety_policy_digest,
            self.accelerator_capability,
            self.minimum_runtime_version,
            str(self.schema_version),
            str(self.policy_epoch),
            str(self.created_unix_millis),
            str(self.expires_unix_millis),
        ]
        lines.extend(f"capability|{capability}" for capability in self.capabilities)
        lines.extend(
            entry.canonical_line()
            for entry in sorted(self.compatibility_classes, key=lambda entry: entry.class_id)
        )
        lines.append(self.envelope.canonical_line())
        return ("\n".join(lines) + "\n").encode()

    @property
    def sealed_digest(self) -> str:
        return "sha256:" + hashlib.sha256(self.canonical_bytes()).hexdigest()

    def verify_digest(self) -> None:
        """Refuse a descriptor whose seal does not match its content."""
        if not _canonical_digest(self.descriptor_digest):
            raise ValueError("model descriptor digest is not canonical")
        if self.descriptor_digest != self.sealed_digest:
            raise ValueError("model descriptor digest does not match its content")

    def servable(self, now_unix_millis: int) -> bool:
        return self.lifecycle == "serving" and now_unix_millis < self.expires_unix_millis

    def compatibility_class(self, class_id: str) -> CompatibilityClass:
        for entry in self.compatibility_classes:
            if entry.class_id == class_id:
                return entry
        raise ValueError("model does not declare the requested compatibility class")


def validate_bundle_against_descriptor(bundle: ModelBundle, descriptor: ModelDescriptor) -> None:
    """Check that the bundle a worker loaded is the one the descriptor names."""
    bundle.validate()
    descriptor.verify_digest()
    if bundle.model_digest != descriptor.model_bundle_digest:
        raise ValueError("loaded model bundle does not match the descriptor")
    if bundle.runtime_digest != descriptor.engine_bundle_digest:
        raise ValueError("loaded runtime bundle does not match the descriptor")
    undeclared = set(bundle.capabilities) - set(descriptor.capabilities)
    if undeclared:
        raise ValueError("loaded bundle claims capabilities the descriptor does not declare")


def validate_batch_against_descriptor(
    batch: BatchPlan,
    descriptor: ModelDescriptor,
    *,
    now_unix_millis: int,
) -> CompatibilityClass:
    """Check a planned batch against the class the model declared.

    Returns the matched class so the caller can reserve against its bounds. This
    is the Python end of ADR-0016: the planner already decided these requests
    share tensors, and this confirms the result stays inside the coarse envelope
    the data plane admitted them under.
    """
    batch.validate()
    descriptor.verify_digest()
    if not descriptor.servable(now_unix_millis):
        raise ValueError("model descriptor is not servable")
    if batch.compatibility.model_bundle_digest != descriptor.model_bundle_digest:
        raise ValueError("batch references a different model bundle")
    for entry in descriptor.compatibility_classes:
        if (
            entry.execution_kind == batch.compatibility.execution_kind
            and entry.precision == batch.compatibility.precision
            and entry.shape_bucket == batch.compatibility.shape_bucket
        ):
            if len(batch.requests) > entry.maximum_batch_requests:
                raise ValueError("batch exceeds the declared class request bound")
            if batch.estimated_gpu_bytes > entry.maximum_batch_gpu_bytes:
                raise ValueError("batch exceeds the declared class GPU bound")
            return entry
    raise ValueError("batch does not match any declared compatibility class")
