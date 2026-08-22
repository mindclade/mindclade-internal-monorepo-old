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


class ExecutionMode(StrEnum):
    """Execution contract used to keep inference-only kernels out of autograd."""

    INFERENCE = "inference"
    TRAINING = "training"


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
        if (
            not isinstance(self.shape, tuple)
            or not self.shape
            or any(
                isinstance(dim, bool) or not isinstance(dim, int) or dim <= 0 for dim in self.shape
            )
        ):
            raise ValueError("tensor shapes must contain only positive dimensions")
        if not isinstance(self.dtype, str) or not self.dtype:
            raise ValueError("dtype must be non-empty")
        if not isinstance(self.layout, TensorLayout):
            raise TypeError("layout must be a TensorLayout")
        if not isinstance(self.contiguous, bool):
            raise TypeError("contiguous must be a boolean")
        if (
            isinstance(self.alignment, bool)
            or not isinstance(self.alignment, int)
            or self.alignment <= 0
        ):
            raise ValueError("dtype must be non-empty and alignment must be positive")

    def canonical(self) -> dict[str, Any]:
        return {
            "alignment": self.alignment,
            "contiguous": self.contiguous,
            "dtype": self.dtype,
            "layout": self.layout.value,
            "shape": list(self.shape),
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> TensorSpec:
        expected = {"alignment", "contiguous", "dtype", "layout", "shape"}
        if not isinstance(payload, dict) or set(payload) != expected:
            raise ValueError("tensor specification contains missing or unknown fields")
        shape = payload["shape"]
        if not isinstance(shape, list):
            raise TypeError("tensor specification shape must be a JSON array")
        return cls(
            shape=tuple(shape),
            dtype=payload["dtype"],
            layout=TensorLayout(payload["layout"]),
            contiguous=payload["contiguous"],
            alignment=payload["alignment"],
        )


@dataclass(frozen=True, slots=True)
class KernelRequest:
    """Exact semantic workload presented to dispatch."""

    operation: str
    inputs: tuple[TensorSpec, ...]
    outputs: tuple[TensorSpec, ...]
    target: str
    architecture: str
    semantics: tuple[tuple[str, str], ...] = field(default_factory=tuple)
    execution_mode: ExecutionMode = ExecutionMode.INFERENCE
    gradient_inputs: tuple[int, ...] = field(default_factory=tuple)
    deterministic: bool = True

    def __post_init__(self) -> None:
        if any(
            not isinstance(value, str) or not value.strip()
            for value in (self.operation, self.target, self.architecture)
        ):
            raise ValueError("operation, target, and architecture are required")
        if not isinstance(self.inputs, tuple) or not self.inputs:
            raise ValueError("kernel requests require at least one input")
        if not isinstance(self.outputs, tuple) or not self.outputs:
            raise ValueError("kernel requests require at least one output")
        if any(
            not isinstance(specification, TensorSpec)
            for specification in (*self.inputs, *self.outputs)
        ):
            raise TypeError("kernel request inputs and outputs must be TensorSpec values")
        if not isinstance(self.execution_mode, ExecutionMode):
            raise TypeError("execution_mode must be an ExecutionMode")
        if not isinstance(self.deterministic, bool):
            raise TypeError("deterministic must be a boolean")
        if any(
            not isinstance(key, str) or not key or not isinstance(value, str)
            for key, value in self.semantics
        ):
            raise TypeError("semantic attributes must contain non-empty string keys and values")
        keys = [key for key, _ in self.semantics]
        if keys != sorted(keys) or len(keys) != len(set(keys)):
            raise ValueError("semantic attributes must have unique, sorted keys")
        if tuple(sorted(set(self.gradient_inputs))) != self.gradient_inputs:
            raise ValueError("gradient input indices must be unique and sorted")
        if any(index < 0 or index >= len(self.inputs) for index in self.gradient_inputs):
            raise ValueError("gradient input indices must refer to request inputs")
        if self.execution_mode == ExecutionMode.INFERENCE and self.gradient_inputs:
            raise ValueError("inference requests cannot require input gradients")
        if self.execution_mode == ExecutionMode.TRAINING and not self.gradient_inputs:
            raise ValueError("training requests must identify differentiable inputs")

    def canonical(self) -> dict[str, Any]:
        return {
            "architecture": self.architecture,
            "deterministic": self.deterministic,
            "execution_mode": self.execution_mode.value,
            "gradient_inputs": list(self.gradient_inputs),
            "inputs": [item.canonical() for item in self.inputs],
            "operation": self.operation,
            "outputs": [item.canonical() for item in self.outputs],
            "semantics": {key: value for key, value in self.semantics},
            "target": self.target,
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> KernelRequest:
        expected = {
            "architecture",
            "deterministic",
            "execution_mode",
            "gradient_inputs",
            "inputs",
            "operation",
            "outputs",
            "semantics",
            "target",
        }
        if not isinstance(payload, dict) or set(payload) != expected:
            raise ValueError("kernel request contains missing or unknown fields")
        inputs = payload["inputs"]
        outputs = payload["outputs"]
        semantics = payload["semantics"]
        gradient_inputs = payload["gradient_inputs"]
        if not isinstance(inputs, list) or not isinstance(outputs, list):
            raise TypeError("kernel request inputs and outputs must be JSON arrays")
        if not isinstance(semantics, dict) or any(
            not isinstance(key, str) or not isinstance(value, str)
            for key, value in semantics.items()
        ):
            raise TypeError("kernel request semantics must be a string mapping")
        if not isinstance(gradient_inputs, list):
            raise TypeError("kernel request gradient_inputs must be a JSON array")
        return cls(
            operation=payload["operation"],
            inputs=tuple(TensorSpec.from_dict(item) for item in inputs),
            outputs=tuple(TensorSpec.from_dict(item) for item in outputs),
            target=payload["target"],
            architecture=payload["architecture"],
            semantics=tuple(sorted(semantics.items())),
            execution_mode=ExecutionMode(payload["execution_mode"]),
            gradient_inputs=tuple(gradient_inputs),
            deterministic=payload["deterministic"],
        )

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
