# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit device capability model used by eligibility predicates."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any


def digest_runtime_environment(values: Mapping[str, Any]) -> str:
    """Return the canonical identity used to bind evidence to a runtime."""

    payload = json.dumps(dict(values), sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(payload.encode()).hexdigest()


@dataclass(frozen=True, slots=True)
class DeviceCapabilities:
    target: str
    architecture: str
    warp_size: int
    max_threads_per_block: int
    shared_memory_per_block: int
    tensor_core_dtypes: frozenset[str]
    supports_async_copy: bool = False
    supports_tma: bool = False
    supports_wgmma: bool = False
    supports_tmem: bool = False
    runtime_environment_digest: str = ""

    def __post_init__(self) -> None:
        if self.warp_size <= 0 or self.max_threads_per_block <= 0:
            raise ValueError("warp and thread limits must be positive")
        if self.shared_memory_per_block <= 0:
            raise ValueError("shared-memory limit must be positive")
        if self.runtime_environment_digest and (
            len(self.runtime_environment_digest) != 64
            or any(c not in "0123456789abcdef" for c in self.runtime_environment_digest)
        ):
            raise ValueError("runtime environment identity must be a lowercase SHA-256 digest")

    def supports_dtype(self, dtype: str) -> bool:
        return dtype in self.tensor_core_dtypes


PORTABLE_CPU = DeviceCapabilities(
    target="cpu",
    architecture="generic",
    warp_size=1,
    max_threads_per_block=1,
    shared_memory_per_block=1,
    tensor_core_dtypes=frozenset(),
    runtime_environment_digest=digest_runtime_environment(
        {"architecture": "generic", "runtime": "python", "target": "cpu"}
    ),
)
