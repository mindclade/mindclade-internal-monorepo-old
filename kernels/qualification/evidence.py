# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import hashlib
import json
import re
from dataclasses import dataclass
from typing import Any

from kernels.api.capabilities import RuntimeCompatibility
from kernels.api.specs import ExecutionMode, KernelRequest
from kernels.qualification.numerical import NumericalEvidence
from kernels.qualification.performance import PerformanceEvidence

_SOURCE_REVISION = re.compile(r"^(?:git:)?(?:[0-9a-f]{40}|[0-9a-f]{64})$")


@dataclass(frozen=True, slots=True)
class QualificationEvidence:
    schema_version: int
    execution_mode: ExecutionMode
    candidate_executed: bool
    fallback_verified: bool
    request: KernelRequest
    paired_request: KernelRequest
    runtime_compatibility: RuntimeCompatibility
    toolchain: str
    request_digest: str
    paired_request_digest: str
    implementation_digest: str
    source_revision: str
    generated_source_digest: str
    artifact_digest: str
    toolchain_digest: str
    environment_digest: str
    numerical: NumericalEvidence
    performance: PerformanceEvidence
    soak_digest: str
    attestation_digest: str
    raw_results_digest: str

    def __post_init__(self) -> None:
        if self.schema_version != 2:
            raise ValueError("qualification evidence schema must be version 2")
        if not isinstance(self.execution_mode, ExecutionMode):
            raise TypeError("execution_mode must be an ExecutionMode")
        if not isinstance(self.candidate_executed, bool) or not isinstance(
            self.fallback_verified, bool
        ):
            raise TypeError("candidate and fallback execution flags must be booleans")
        if not isinstance(self.request, KernelRequest) or not isinstance(
            self.paired_request, KernelRequest
        ):
            raise TypeError("qualification evidence requires canonical KernelRequest documents")
        if not isinstance(self.runtime_compatibility, RuntimeCompatibility):
            raise TypeError("qualification evidence requires RuntimeCompatibility")
        if not isinstance(self.toolchain, str) or not self.toolchain.strip():
            raise ValueError("qualification toolchain identity must be non-empty")
        for name, digest in (
            ("request_digest", self.request_digest),
            ("paired_request_digest", self.paired_request_digest),
            ("implementation_digest", self.implementation_digest),
            ("generated_source_digest", self.generated_source_digest),
            ("artifact_digest", self.artifact_digest),
            ("toolchain_digest", self.toolchain_digest),
            ("environment_digest", self.environment_digest),
            ("soak_digest", self.soak_digest),
            ("attestation_digest", self.attestation_digest),
            ("raw_results_digest", self.raw_results_digest),
        ):
            if len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
                raise ValueError(f"{name} must be a lowercase SHA-256 digest")
        if self.request_digest == self.paired_request_digest:
            raise ValueError("paired qualification requests must be distinct")
        if self.request_digest != self.request.digest:
            raise ValueError("request_digest does not match the canonical request")
        if self.paired_request_digest != self.paired_request.digest:
            raise ValueError("paired_request_digest does not match the canonical paired request")
        if self.execution_mode != self.request.execution_mode:
            raise ValueError("evidence execution mode does not match its canonical request")
        expected_paired_mode = (
            ExecutionMode.TRAINING
            if self.execution_mode == ExecutionMode.INFERENCE
            else ExecutionMode.INFERENCE
        )
        if self.paired_request.execution_mode != expected_paired_mode:
            raise ValueError("paired request must cover the reciprocal execution mode")
        request_contract = (
            self.request.operation,
            self.request.inputs,
            self.request.outputs,
            self.request.target,
            self.request.architecture,
            self.request.semantics,
            self.request.deterministic,
        )
        paired_contract = (
            self.paired_request.operation,
            self.paired_request.inputs,
            self.paired_request.outputs,
            self.paired_request.target,
            self.paired_request.architecture,
            self.paired_request.semantics,
            self.paired_request.deterministic,
        )
        if request_contract != paired_contract:
            raise ValueError("paired requests must describe the same kernel contract")
        if (
            self.request.target != self.runtime_compatibility.target
            or self.request.architecture != self.runtime_compatibility.architecture
        ):
            raise ValueError("canonical request target does not match runtime compatibility")
        if self.environment_digest != self.runtime_compatibility.digest:
            raise ValueError("environment_digest does not match runtime compatibility")
        if self.toolchain_digest != hashlib.sha256(self.toolchain.encode()).hexdigest():
            raise ValueError("toolchain_digest does not match the canonical toolchain")
        if _SOURCE_REVISION.fullmatch(self.source_revision) is None:
            raise ValueError("source_revision must be an immutable hexadecimal Git revision")
        if self.execution_mode == ExecutionMode.INFERENCE and self.numerical.gradient_inputs:
            raise ValueError("inference evidence cannot claim input gradients")
        if self.execution_mode == ExecutionMode.INFERENCE and not self.candidate_executed:
            raise ValueError("inference evidence must execute the candidate")
        if self.execution_mode == ExecutionMode.TRAINING and not self.numerical.gradient_inputs:
            raise ValueError("training evidence must exercise input gradients")
        if (
            self.execution_mode == ExecutionMode.TRAINING
            and not self.candidate_executed
            and not self.fallback_verified
        ):
            raise ValueError("training evidence must execute a candidate or verify fallback")
        if self.candidate_executed and self.fallback_verified:
            raise ValueError("one evidence record cannot execute both candidate and fallback")

    def canonical(self) -> dict[str, object]:
        return {
            "artifact_digest": self.artifact_digest,
            "attestation_digest": self.attestation_digest,
            "candidate_executed": self.candidate_executed,
            "environment_digest": self.environment_digest,
            "execution_mode": self.execution_mode.value,
            "fallback_verified": self.fallback_verified,
            "generated_source_digest": self.generated_source_digest,
            "implementation_digest": self.implementation_digest,
            "numerical": self.numerical.canonical(),
            "paired_request_digest": self.paired_request_digest,
            "performance": self.performance.canonical(),
            "paired_request": self.paired_request.canonical(),
            "raw_results_digest": self.raw_results_digest,
            "request": self.request.canonical(),
            "request_digest": self.request_digest,
            "runtime_compatibility": self.runtime_compatibility.canonical(),
            "schema_version": self.schema_version,
            "soak_digest": self.soak_digest,
            "source_revision": self.source_revision,
            "toolchain": self.toolchain,
            "toolchain_digest": self.toolchain_digest,
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> QualificationEvidence:
        expected = {
            "artifact_digest",
            "attestation_digest",
            "candidate_executed",
            "environment_digest",
            "execution_mode",
            "fallback_verified",
            "generated_source_digest",
            "implementation_digest",
            "numerical",
            "paired_request",
            "paired_request_digest",
            "performance",
            "raw_results_digest",
            "request",
            "request_digest",
            "runtime_compatibility",
            "schema_version",
            "soak_digest",
            "source_revision",
            "toolchain",
            "toolchain_digest",
        }
        if not isinstance(payload, dict) or set(payload) != expected:
            raise ValueError("qualification evidence contains missing or unknown fields")
        return cls(
            schema_version=payload["schema_version"],
            execution_mode=ExecutionMode(payload["execution_mode"]),
            candidate_executed=payload["candidate_executed"],
            fallback_verified=payload["fallback_verified"],
            request=KernelRequest.from_dict(payload["request"]),
            paired_request=KernelRequest.from_dict(payload["paired_request"]),
            runtime_compatibility=RuntimeCompatibility.from_dict(payload["runtime_compatibility"]),
            toolchain=payload["toolchain"],
            request_digest=payload["request_digest"],
            paired_request_digest=payload["paired_request_digest"],
            implementation_digest=payload["implementation_digest"],
            source_revision=payload["source_revision"],
            generated_source_digest=payload["generated_source_digest"],
            artifact_digest=payload["artifact_digest"],
            toolchain_digest=payload["toolchain_digest"],
            environment_digest=payload["environment_digest"],
            numerical=NumericalEvidence.from_dict(payload["numerical"]),
            performance=PerformanceEvidence.from_dict(payload["performance"]),
            soak_digest=payload["soak_digest"],
            attestation_digest=payload["attestation_digest"],
            raw_results_digest=payload["raw_results_digest"],
        )

    @property
    def digest(self) -> str:
        payload = {
            "artifact_digest": self.artifact_digest,
            "attestation_digest": self.attestation_digest,
            "candidate_executed": self.candidate_executed,
            "environment_digest": self.environment_digest,
            "execution_mode": self.execution_mode.value,
            "fallback_verified": self.fallback_verified,
            "generated_source_digest": self.generated_source_digest,
            "implementation_digest": self.implementation_digest,
            "numerical_digest": self.numerical.digest,
            "paired_request_digest": self.paired_request_digest,
            "performance_digest": self.performance.digest,
            "paired_request_document_digest": self.paired_request.digest,
            "raw_results_digest": self.raw_results_digest,
            "request_digest": self.request_digest,
            "request_document_digest": self.request.digest,
            "runtime_compatibility_digest": self.runtime_compatibility.digest,
            "schema_version": self.schema_version,
            "soak_digest": self.soak_digest,
            "source_revision": self.source_revision,
            "toolchain": self.toolchain,
            "toolchain_digest": self.toolchain_digest,
        }
        encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
        return hashlib.sha256(encoded).hexdigest()
