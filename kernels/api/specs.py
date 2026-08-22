# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Provider-neutral workload and implementation identities."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Any


class Provider(StrEnum):
    PYTORCH = "pytorch"
    TILELANG = "tilelang"
    VENDOR = "vendor"


class TensorLayout(StrEnum):
    CONTIGUOUS = "contiguous"
    BHSD = "bhsd"
    BSHD = "bshd"
    PAIR_MAJOR = "bnmc"
    EXPERT_MAJOR = "emk"


@dataclass(frozen=True, slots=True)
class TensorSpec:
    shape: tuple[int, ...]
    dtype: str
    layout: TensorLayout = TensorLayout.CONTIGUOUS
    contiguous: bool = True
    alignment: int = 1

    def __post_init__(self) -> None:
        if not self.shape or any(dim <= 0 for dim in self.shape):
            raise ValueError("tensor shapes must contain only positive dimensions")
        if not self.dtype or self.alignment <= 0:
            raise ValueError("dtype must be non-empty and alignment must be positive")

    def canonical(self) -> dict[str, Any]:
        return {
            "alignment": self.alignment,
            "contiguous": self.contiguous,
            "dtype": self.dtype,
            "layout": self.layout.value,
            "shape": list(self.shape),
        }


@dataclass(frozen=True, slots=True)
class KernelRequest:
    """Exact semantic workload presented to dispatch."""

    operation: str
    inputs: tuple[TensorSpec, ...]
    outputs: tuple[TensorSpec, ...]
    target: str
    architecture: str
    semantics: tuple[tuple[str, str], ...] = field(default_factory=tuple)

    def __post_init__(self) -> None:
        if not self.operation or not self.target or not self.architecture:
            raise ValueError("operation, target, and architecture are required")
        keys = [key for key, _ in self.semantics]
        if keys != sorted(keys) or len(keys) != len(set(keys)):
            raise ValueError("semantic attributes must have unique, sorted keys")

    def canonical(self) -> dict[str, Any]:
        return {
            "architecture": self.architecture,
            "inputs": [item.canonical() for item in self.inputs],
            "operation": self.operation,
            "outputs": [item.canonical() for item in self.outputs],
            "semantics": {key: value for key, value in self.semantics},
            "target": self.target,
        }

    @property
    def digest(self) -> str:
        payload = json.dumps(self.canonical(), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode("utf-8")).hexdigest()


@dataclass(frozen=True, slots=True)
class ImplementationIdentity:
    provider: Provider
    name: str
    source_digest: str
    compiler: str
    compiler_version: str
    schedule_digest: str

    def __post_init__(self) -> None:
        digest_fields = (self.source_digest, self.schedule_digest)
        if any(
            len(value) != 64 or any(c not in "0123456789abcdef" for c in value)
            for value in digest_fields
        ):
            raise ValueError("source and schedule digests must be lowercase SHA-256 values")
        if not self.name or not self.compiler or not self.compiler_version:
            raise ValueError("implementation name and compiler identity are required")

    @property
    def digest(self) -> str:
        payload = {
            "compiler": self.compiler,
            "compiler_version": self.compiler_version,
            "name": self.name,
            "provider": self.provider.value,
            "schedule_digest": self.schedule_digest,
            "source_digest": self.source_digest,
        }
        encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
        return hashlib.sha256(encoded).hexdigest()
