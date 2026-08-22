# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thread-safe registry for exact, side-effect-free kernel selection."""

from __future__ import annotations

import hashlib
from collections.abc import Callable
from dataclasses import dataclass
from threading import RLock
from typing import Any

from kernels.api.capabilities import DeviceCapabilities
from kernels.api.specs import ExecutionMode, ImplementationIdentity, KernelRequest, Provider

Eligibility = Callable[[KernelRequest, DeviceCapabilities], str | None]
KernelCallable = Callable[..., Any]


@dataclass(frozen=True, slots=True)
class KernelImplementation:
    operation: str
    identity: ImplementationIdentity
    invoke: KernelCallable
    eligibility: Eligibility
    priority: int = 0
    execution_modes: frozenset[ExecutionMode] = frozenset({ExecutionMode.INFERENCE})
    differentiable_inputs: frozenset[int] = frozenset()
    deterministic: bool = True
    artifact_digest: str | None = None

    def __post_init__(self) -> None:
        if not self.operation.strip():
            raise ValueError("kernel operation must be non-empty")
        if isinstance(self.priority, bool) or not isinstance(self.priority, int):
            raise TypeError("kernel priority must be an integer")
        if self.artifact_digest is not None and (
            len(self.artifact_digest) != 64
            or any(character not in "0123456789abcdef" for character in self.artifact_digest)
        ):
            raise ValueError("kernel artifact identity must be a lowercase SHA-256 digest")

    @property
    def toolchain_digest(self) -> str:
        toolchain = f"{self.identity.compiler}-{self.identity.compiler_version}"
        return hashlib.sha256(toolchain.encode()).hexdigest()

    @property
    def qualified_artifact_digest(self) -> str:
        """Return the explicit deployed artifact digest.

        Source identity is deliberately not a substitute: JIT compilation can
        produce a different binary after dispatch has selected a candidate.
        """

        if self.artifact_digest is None:
            raise ValueError("kernel implementation has no bound compiled artifact")
        return self.artifact_digest

    def rejection_reason(
        self, request: KernelRequest, capabilities: DeviceCapabilities
    ) -> str | None:
        if request.operation != self.operation:
            return "operation_mismatch"
        if request.execution_mode not in self.execution_modes:
            return "execution_mode"
        if not set(request.gradient_inputs).issubset(self.differentiable_inputs):
            return "gradient_contract"
        if request.deterministic and not self.deterministic:
            return "determinism"
        return self.eligibility(request, capabilities)


class KernelRegistry:
    """A registry whose snapshots are immutable from a dispatch caller's view."""

    def __init__(self) -> None:
        self._lock = RLock()
        self._implementations: dict[str, KernelImplementation] = {}

    def register(self, implementation: KernelImplementation) -> None:
        key = implementation.identity.digest
        with self._lock:
            if key in self._implementations:
                raise ValueError(
                    f"implementation {implementation.identity.name!r} already registered"
                )
            self._implementations[key] = implementation

    def candidates(self, operation: str) -> tuple[KernelImplementation, ...]:
        with self._lock:
            candidates = tuple(
                implementation
                for implementation in self._implementations.values()
                if implementation.operation == operation
            )
        return tuple(
            sorted(
                candidates,
                key=lambda item: (
                    item.identity.provider == Provider.PYTORCH,
                    -item.priority,
                    item.identity.digest,
                ),
            )
        )

    def reference(self, operation: str) -> KernelImplementation:
        references = [
            item
            for item in self.candidates(operation)
            if item.identity.provider == Provider.PYTORCH
        ]
        if len(references) != 1:
            raise LookupError(
                f"operation {operation!r} must have exactly one PyTorch reference, found {len(references)}"
            )
        return references[0]
