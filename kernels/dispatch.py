# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Qualification-gated dispatch with explicit fallback reasons."""

from __future__ import annotations

import os
from collections.abc import Callable, Mapping
from dataclasses import dataclass

from kernels.api.capabilities import DeviceCapabilities
from kernels.api.specs import KernelRequest, Provider
from kernels.manifest import QualificationManifest, QualificationRecord
from kernels.registry import KernelImplementation, KernelRegistry

TILELANG_KILL_SWITCH = "MINDCLADE_DISABLE_TILELANG"
TILELANG_OPERATION_KILL_SWITCH = "MINDCLADE_DISABLE_TILELANG_OPERATIONS"


@dataclass(frozen=True, slots=True)
class Rejection:
    implementation: str
    reason: str


@dataclass(frozen=True, slots=True)
class DispatchEvent:
    request_digest: str
    operation: str
    selected_implementation: str
    selected_provider: Provider
    used_fallback: bool
    rejection_reasons: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class DispatchDecision:
    implementation: KernelImplementation
    qualification: QualificationRecord | None
    rejections: tuple[Rejection, ...]

    @property
    def used_fallback(self) -> bool:
        return self.implementation.identity.provider == Provider.PYTORCH


class KernelDispatcher:
    def __init__(
        self,
        registry: KernelRegistry,
        manifest: QualificationManifest,
        *,
        environment: Mapping[str, str] | None = None,
        event_sink: Callable[[DispatchEvent], None] | None = None,
    ) -> None:
        self._registry = registry
        self._manifest = manifest
        # Snapshot the process environment at construction so the documented
        # emergency switches work in real callers while a supplied mapping
        # remains deterministic in tests and dependency-injected runtimes.
        self._environment = dict(os.environ if environment is None else environment)
        self._event_sink = event_sink

    def _decision(
        self,
        request: KernelRequest,
        implementation: KernelImplementation,
        qualification: QualificationRecord | None,
        rejections: list[Rejection],
    ) -> DispatchDecision:
        decision = DispatchDecision(implementation, qualification, tuple(rejections))
        if self._event_sink is not None:
            self._event_sink(
                DispatchEvent(
                    request_digest=request.digest,
                    operation=request.operation,
                    selected_implementation=implementation.identity.name,
                    selected_provider=implementation.identity.provider,
                    used_fallback=decision.used_fallback,
                    rejection_reasons=tuple(
                        f"{item.implementation}:{item.reason}" for item in rejections
                    ),
                )
            )
        return decision

    def select(self, request: KernelRequest, capabilities: DeviceCapabilities) -> DispatchDecision:
        rejections: list[Rejection] = []
        disabled = self._environment.get(TILELANG_KILL_SWITCH, "").lower() in {
            "1",
            "true",
            "yes",
        }
        disabled_operations = {
            operation.strip()
            for operation in self._environment.get(TILELANG_OPERATION_KILL_SWITCH, "").split(",")
            if operation.strip()
        }

        for candidate in self._registry.candidates(request.operation):
            if candidate.identity.provider == Provider.PYTORCH:
                continue
            if candidate.identity.provider == Provider.TILELANG and (
                disabled or request.operation in disabled_operations
            ):
                rejections.append(Rejection(candidate.identity.name, "kill_switch"))
                continue
            reason = candidate.rejection_reason(request, capabilities)
            if reason is not None:
                rejections.append(Rejection(candidate.identity.name, reason))
                continue
            if candidate.artifact_digest is None:
                rejections.append(Rejection(candidate.identity.name, "artifact_unbound"))
                continue
            qualification = self._manifest.qualification(request.digest, candidate.identity.digest)
            if qualification is None:
                rejections.append(Rejection(candidate.identity.name, "unqualified"))
                continue
            if qualification.execution_mode != request.execution_mode:
                rejections.append(
                    Rejection(candidate.identity.name, "qualification_execution_mode")
                )
                continue
            if (
                qualification.target != request.target
                or qualification.target != capabilities.target
                or qualification.architecture != request.architecture
                or qualification.architecture != capabilities.architecture
            ):
                rejections.append(Rejection(candidate.identity.name, "qualification_target"))
                continue
            expected_toolchain = (
                f"{candidate.identity.compiler}-{candidate.identity.compiler_version}"
            )
            if qualification.toolchain != expected_toolchain:
                rejections.append(Rejection(candidate.identity.name, "qualification_toolchain"))
                continue
            if qualification.toolchain_digest != candidate.toolchain_digest:
                rejections.append(
                    Rejection(candidate.identity.name, "qualification_toolchain_digest")
                )
                continue
            if qualification.artifact_digest != candidate.artifact_digest:
                rejections.append(Rejection(candidate.identity.name, "qualification_artifact"))
                continue
            if (
                not capabilities.runtime_environment_digest
                or qualification.environment_digest != capabilities.runtime_environment_digest
            ):
                rejections.append(Rejection(candidate.identity.name, "qualification_environment"))
                continue
            return self._decision(request, candidate, qualification, rejections)

        fallback = self._registry.reference(request.operation)
        reason = fallback.rejection_reason(request, capabilities)
        if reason is not None:
            raise LookupError(
                f"reference implementation {fallback.identity.name!r} rejected request: {reason}"
            )
        return self._decision(request, fallback, None, rejections)
