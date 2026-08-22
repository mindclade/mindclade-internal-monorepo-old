# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thread-safe registry for exact, side-effect-free kernel selection."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from threading import RLock
from typing import Any

from kernels.api.capabilities import DeviceCapabilities
from kernels.api.specs import ImplementationIdentity, KernelRequest, Provider

Eligibility = Callable[[KernelRequest, DeviceCapabilities], str | None]
KernelCallable = Callable[..., Any]


@dataclass(frozen=True, slots=True)
class KernelImplementation:
    operation: str
    identity: ImplementationIdentity
    invoke: KernelCallable
    eligibility: Eligibility
    priority: int = 0

    def rejection_reason(
        self, request: KernelRequest, capabilities: DeviceCapabilities
    ) -> str | None:
        if request.operation != self.operation:
            return "operation_mismatch"
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
