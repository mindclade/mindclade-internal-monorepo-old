# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Explicit device capability model used by eligibility predicates."""

from __future__ import annotations

import hashlib
import json
import re
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

_OCI_IMAGE_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")


def digest_runtime_environment(values: Mapping[str, Any]) -> str:
    """Return the canonical identity used to bind evidence to a runtime."""

    payload = json.dumps(dict(values), sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(payload.encode()).hexdigest()


@dataclass(frozen=True, slots=True)
class RuntimeCompatibility:
    """Stable fields that must match before qualification can be reused."""

    target: str
    architecture: str
    device_name: str
    device_memory_bytes: int
    driver_version: str
    runtime_version: str
    pytorch_version: str
    tilelang_version: str
    tvm_ffi_version: str
    compiler_version: str
    os_release: str
    runtime_image_digest: str
    partition_profile: str = "none"

    def __post_init__(self) -> None:
        string_fields = (
            self.target,
            self.architecture,
            self.device_name,
            self.driver_version,
            self.runtime_version,
            self.pytorch_version,
            self.tilelang_version,
            self.tvm_ffi_version,
            self.compiler_version,
            self.os_release,
            self.runtime_image_digest,
            self.partition_profile,
        )
        if any(not isinstance(value, str) or not value.strip() for value in string_fields):
            raise ValueError("runtime compatibility fields must be non-empty")
        if (
            isinstance(self.device_memory_bytes, bool)
            or not isinstance(self.device_memory_bytes, int)
            or self.device_memory_bytes <= 0
        ):
            raise ValueError("device memory must be positive")
        if _OCI_IMAGE_DIGEST.fullmatch(self.runtime_image_digest) is None:
            raise ValueError(
                "runtime_image_digest must be a sha256:<64 lowercase hexadecimal> identity"
            )

    def canonical(self) -> dict[str, Any]:
        return {
            "architecture": self.architecture,
            "compiler_version": self.compiler_version,
            "device_memory_bytes": self.device_memory_bytes,
            "device_name": self.device_name,
            "driver_version": self.driver_version,
            "os_release": self.os_release,
            "partition_profile": self.partition_profile,
            "pytorch_version": self.pytorch_version,
            "runtime_image_digest": self.runtime_image_digest,
            "runtime_version": self.runtime_version,
            "target": self.target,
            "tilelang_version": self.tilelang_version,
            "tvm_ffi_version": self.tvm_ffi_version,
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> RuntimeCompatibility:
        expected = {
            "architecture",
            "compiler_version",
            "device_memory_bytes",
            "device_name",
            "driver_version",
            "os_release",
            "partition_profile",
            "pytorch_version",
            "runtime_image_digest",
            "runtime_version",
            "target",
            "tilelang_version",
            "tvm_ffi_version",
        }
        if not isinstance(payload, dict) or set(payload) != expected:
            raise ValueError("runtime compatibility contains missing or unknown fields")
        return cls(**payload)

    @property
    def digest(self) -> str:
        return digest_runtime_environment(self.canonical())


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
