# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import hashlib
import inspect
from collections.abc import Callable
from functools import partial

from kernels.api.specs import ImplementationIdentity, Provider
from kernels.registry import KernelImplementation

_REFERENCE_SCHEDULE_DIGEST = hashlib.sha256(b"pytorch-reference-v1").hexdigest()


def reference_registration(operation: str, invoke: Callable[..., object]) -> KernelImplementation:
    source_callable = invoke.func if isinstance(invoke, partial) else invoke
    binding = repr((invoke.args, invoke.keywords)) if isinstance(invoke, partial) else ""
    identity = ImplementationIdentity(
        provider=Provider.PYTORCH,
        name=f"pytorch.{operation}",
        source_digest=hashlib.sha256(
            (inspect.getsource(source_callable) + binding).encode()
        ).hexdigest(),
        compiler="pytorch",
        compiler_version="2.13",
        schedule_digest=_REFERENCE_SCHEDULE_DIGEST,
    )
    return KernelImplementation(
        operation=operation,
        identity=identity,
        invoke=invoke,
        eligibility=lambda _request, _capabilities: None,
        priority=-1,
    )
