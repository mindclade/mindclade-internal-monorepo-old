# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Helpers for content-addressed TileLang registrations."""

from __future__ import annotations

import hashlib
import inspect
from collections.abc import Callable

from kernels.api.specs import ImplementationIdentity, Provider
from kernels.providers.tilelang.manifest import TILELANG_VERSION
from kernels.registry import Eligibility, KernelImplementation


def implementation_identity(name: str, factory: Callable[..., object], schedule_digest: str) -> ImplementationIdentity:
    source = inspect.getsource(factory).encode()
    return ImplementationIdentity(
        provider=Provider.TILELANG,
        name=name,
        source_digest=hashlib.sha256(source).hexdigest(),
        compiler="tilelang",
        compiler_version=TILELANG_VERSION,
        schedule_digest=schedule_digest,
    )


def registration(
    *,
    operation: str,
    name: str,
    factory: Callable[..., object],
    schedule_digest: str,
    invoke: Callable[..., object],
    eligibility: Eligibility,
    priority: int,
) -> KernelImplementation:
    return KernelImplementation(
        operation=operation,
        identity=implementation_identity(name, factory, schedule_digest),
        invoke=invoke,
        eligibility=eligibility,
        priority=priority,
    )
