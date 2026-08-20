# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Qualification-gated dispatch with explicit fallback reasons."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass

from kernels.api.capabilities import DeviceCapabilities
from kernels.api.specs import KernelRequest, Provider
from kernels.manifest import QualificationManifest, QualificationRecord
from kernels.registry import KernelImplementation, KernelRegistry

TILELANG_KILL_SWITCH = "MINDCLADE_DISABLE_TILELANG"


@dataclass(frozen=True, slots=True)
class Rejection:
    implementation: str
    reason: str


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
    ) -> None:
        self._registry = registry
        self._manifest = manifest
        self._environment = dict(environment or {})

    def select(
        self, request: KernelRequest, capabilities: DeviceCapabilities
    ) -> DispatchDecision:
        rejections: list[Rejection] = []
        disabled = self._environment.get(TILELANG_KILL_SWITCH, "").lower() in {
            "1",
            "true",
            "yes",
        }

        for candidate in self._registry.candidates(request.operation):
            if candidate.identity.provider == Provider.PYTORCH:
                continue
            if candidate.identity.provider == Provider.TILELANG and disabled:
                rejections.append(Rejection(candidate.identity.name, "kill_switch"))
                continue
            reason = candidate.rejection_reason(request, capabilities)
            if reason is not None:
                rejections.append(Rejection(candidate.identity.name, reason))
                continue
            qualification = self._manifest.qualification(
                request.digest, candidate.identity.digest
            )
            if qualification is None:
                rejections.append(Rejection(candidate.identity.name, "unqualified"))
                continue
            return DispatchDecision(candidate, qualification, tuple(rejections))

        fallback = self._registry.reference(request.operation)
        reason = fallback.rejection_reason(request, capabilities)
        if reason is not None:
            raise LookupError(
                f"reference implementation {fallback.identity.name!r} rejected request: {reason}"
            )
        return DispatchDecision(fallback, None, tuple(rejections))
