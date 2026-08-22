# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic PyTorch reference implementation of the affine model contract.

This module is deliberately small. It provides a real, stateful ``nn.Module`` for exercising
model, training, serialization, and serving boundaries without representing a scientific model.
The caller owns device placement; forward execution never casts or moves tensors implicitly.
"""

from __future__ import annotations

import json
import math
import os
import stat
import tempfile
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Final

import torch
from safetensors.torch import load as load_safetensors
from safetensors.torch import save_file
from torch import nn

from libs.python.serialization import canonical_json_bytes

REFERENCE_AFFINE_MODEL_NAME: Final = "reference-affine-v1"
REFERENCE_AFFINE_OPERATION: Final = "reference.affine.v1"
REFERENCE_AFFINE_DTYPE: Final = "float32"
REFERENCE_AFFINE_CONFIG_SCHEMA_VERSION: Final = 1
DEFAULT_MAXIMUM_INPUT_ELEMENTS: Final = 16_777_216
MAXIMUM_REFERENCE_CHECKPOINT_BYTES: Final = 1 << 20
MAXIMUM_REFERENCE_CONFIG_BYTES: Final = 4 << 10

_STATE_KEYS: Final = frozenset({"scale", "bias"})
_CONFIG_KEYS: Final = frozenset(
    {
        "architecture",
        "dtype",
        "maximum_input_elements",
        "operation",
        "schema_version",
    }
)


@dataclass(frozen=True, slots=True)
class ReferenceAffineConfig:
    """Immutable construction and input-budget configuration for ``ReferenceAffine``."""

    scale: float = 2.0
    bias: float = 0.5
    dtype: str = REFERENCE_AFFINE_DTYPE
    operation: str = REFERENCE_AFFINE_OPERATION
    maximum_input_elements: int = DEFAULT_MAXIMUM_INPUT_ELEMENTS

    def __post_init__(self) -> None:
        for name, value in (("scale", self.scale), ("bias", self.bias)):
            if isinstance(value, bool) or not isinstance(value, (int, float)):
                raise TypeError(f"reference affine {name} must be a real number")
            numeric_value = float(value)
            if not math.isfinite(numeric_value):
                raise ValueError(f"reference affine {name} must be finite")
            if abs(numeric_value) > torch.finfo(torch.float32).max:
                raise ValueError(f"reference affine {name} is outside the float32 range")
            object.__setattr__(self, name, numeric_value)

        if self.dtype != REFERENCE_AFFINE_DTYPE:
            raise ValueError("reference affine dtype must be float32")
        if self.operation != REFERENCE_AFFINE_OPERATION:
            raise ValueError(f"reference affine operation must be {REFERENCE_AFFINE_OPERATION}")
        if isinstance(self.maximum_input_elements, bool) or not isinstance(
            self.maximum_input_elements, int
        ):
            raise TypeError("reference affine maximum_input_elements must be an integer")
        if self.maximum_input_elements <= 0:
            raise ValueError("reference affine maximum_input_elements must be positive")
        if self.maximum_input_elements > DEFAULT_MAXIMUM_INPUT_ELEMENTS:
            raise ValueError("reference affine maximum_input_elements exceeds the v1 limit")


class ReferenceAffine(nn.Module):
    """Apply ``output = (input * scale) + bias`` without hidden casts or transfers."""

    scale: nn.Parameter
    bias: nn.Parameter

    def __init__(self, config: ReferenceAffineConfig | None = None) -> None:
        super().__init__()
        resolved_config = config if config is not None else ReferenceAffineConfig()
        if not isinstance(resolved_config, ReferenceAffineConfig):
            raise TypeError("config must be a ReferenceAffineConfig")
        self._config = resolved_config
        self.scale = nn.Parameter(torch.tensor(resolved_config.scale, dtype=torch.float32))
        self.bias = nn.Parameter(torch.tensor(resolved_config.bias, dtype=torch.float32))

    @property
    def config(self) -> ReferenceAffineConfig:
        """Return the immutable configuration used to construct the module."""

        return self._config

    def forward(self, inputs: torch.Tensor) -> torch.Tensor:
        """Evaluate the affine operation while preserving input shape, dtype, and device."""

        if not isinstance(inputs, torch.Tensor):
            raise TypeError("reference affine input must be a torch.Tensor")
        if inputs.dtype != torch.float32:
            raise TypeError("reference affine input must have dtype torch.float32")
        if inputs.layout != torch.strided:
            raise TypeError("reference affine input must use a strided layout")
        if inputs.numel() == 0:
            raise ValueError("reference affine input must be nonempty")
        if inputs.numel() > self._config.maximum_input_elements:
            raise ValueError("reference affine input exceeds maximum_input_elements")

        self._validate_parameters()
        if inputs.device != self.scale.device:
            raise ValueError("reference affine input and parameters must be on the same device")
        if not torch.isfinite(inputs).all().item():
            raise ValueError("reference affine input must contain only finite values")
        outputs = self.compute(inputs)
        if not torch.isfinite(outputs).all().item():
            raise FloatingPointError("reference affine arithmetic produced non-finite output")
        return outputs

    def compute(self, inputs: torch.Tensor) -> torch.Tensor:
        """Apply model-owned arithmetic after an adapter has validated its tensor boundary.

        Training should normally call the module so ``forward`` enforces the complete public
        contract. Runtime adapters that validate their boundary once may call this method for
        the single v1 operation and then validate the final output, avoiding duplicate scans and
        host synchronizations.
        """

        return (inputs * self.scale) + self.bias

    def validate_state(self) -> None:
        """Fail closed if an adapter-loaded parameter state violates the model contract."""

        self._validate_parameters()

    def _validate_parameters(self) -> None:
        for name, parameter in (("scale", self.scale), ("bias", self.bias)):
            if parameter.dtype != torch.float32:
                raise RuntimeError(f"reference affine {name} parameter must remain float32")
            if parameter.shape != torch.Size([]):
                raise RuntimeError(f"reference affine {name} parameter must remain scalar")
            if parameter.layout != torch.strided:
                raise RuntimeError(f"reference affine {name} parameter must use strided layout")
            if not torch.isfinite(parameter).item():
                raise RuntimeError(f"reference affine {name} parameter must remain finite")
        if self.scale.device != self.bias.device:
            raise RuntimeError("reference affine parameters must be on the same device")

    def extra_repr(self) -> str:
        return (
            f"operation={self._config.operation!r}, dtype={self._config.dtype!r}, "
            f"maximum_input_elements={self._config.maximum_input_elements}"
        )


def reference_affine_config_document(config: ReferenceAffineConfig) -> dict[str, object]:
    """Return the exact model-owned v1 bundle configuration document."""

    if not isinstance(config, ReferenceAffineConfig):
        raise TypeError("config must be a ReferenceAffineConfig")
    return {
        "architecture": REFERENCE_AFFINE_MODEL_NAME,
        "dtype": REFERENCE_AFFINE_DTYPE,
        "maximum_input_elements": config.maximum_input_elements,
        "operation": REFERENCE_AFFINE_OPERATION,
        "schema_version": REFERENCE_AFFINE_CONFIG_SCHEMA_VERSION,
    }


def reference_affine_config_bytes(config: ReferenceAffineConfig) -> bytes:
    """Encode the exact model-owned v1 bundle configuration."""

    return canonical_json_bytes(
        reference_affine_config_document(config),
        maximum_encoded_bytes=MAXIMUM_REFERENCE_CONFIG_BYTES,
    )


def parse_reference_affine_config(value: bytes) -> ReferenceAffineConfig:
    """Parse an exact, unique-key v1 bundle config into an immutable model config."""

    if not isinstance(value, bytes):
        raise TypeError("reference affine config must be bytes")
    if not value or len(value) > MAXIMUM_REFERENCE_CONFIG_BYTES:
        raise ValueError("reference affine config size is outside bounds")
    try:
        document = json.loads(
            value,
            object_pairs_hook=_unique_json_object,
            parse_constant=_reject_json_constant,
        )
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
        raise ValueError("reference affine config must be unique-key UTF-8 JSON") from error
    if not isinstance(document, dict) or set(document) != _CONFIG_KEYS:
        raise ValueError("reference affine config fields do not match schema v1")
    exact = {
        "architecture": REFERENCE_AFFINE_MODEL_NAME,
        "dtype": REFERENCE_AFFINE_DTYPE,
        "operation": REFERENCE_AFFINE_OPERATION,
        "schema_version": REFERENCE_AFFINE_CONFIG_SCHEMA_VERSION,
    }
    if (
        isinstance(document["schema_version"], bool)
        or not isinstance(document["schema_version"], int)
        or any(document[name] != expected for name, expected in exact.items())
    ):
        raise ValueError("reference affine config identity does not match schema v1")
    maximum_input_elements = document["maximum_input_elements"]
    if isinstance(maximum_input_elements, bool) or not isinstance(maximum_input_elements, int):
        raise ValueError("reference affine maximum_input_elements must be an integer")
    config = ReferenceAffineConfig(maximum_input_elements=maximum_input_elements)
    if reference_affine_config_bytes(config) != value:
        raise ValueError("reference affine config must use canonical JSON bytes")
    return config


def _unique_json_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    document: dict[str, object] = {}
    for key, value in pairs:
        if key in document:
            raise ValueError(f"duplicate JSON key {key!r}")
        document[key] = value
    return document


def _reject_json_constant(value: str) -> object:
    raise ValueError(f"non-finite JSON number {value}")


def save_reference_affine(
    model: ReferenceAffine,
    destination: str | os.PathLike[str],
) -> Path:
    """Atomically save exact affine state using the non-executable safetensors format.

    Tensor serialization may stage device data internally, but this helper never mutates or
    relocates the model itself. The destination parent must already exist.
    """

    if not isinstance(model, ReferenceAffine):
        raise TypeError("model must be a ReferenceAffine")
    model._validate_parameters()
    destination_path = _validate_save_destination(destination)
    state = _validated_state(model.state_dict())
    serialized_state = {key: tensor.detach().contiguous() for key, tensor in state.items()}
    metadata = {
        "format": "pt",
        "mindclade.model": REFERENCE_AFFINE_MODEL_NAME,
        "mindclade.operation": REFERENCE_AFFINE_OPERATION,
    }

    descriptor, temporary_name = tempfile.mkstemp(
        dir=destination_path.parent,
        prefix=f".{destination_path.name}.",
        suffix=".tmp",
    )
    os.close(descriptor)
    temporary_path = Path(temporary_name)
    try:
        save_file(serialized_state, str(temporary_path), metadata=metadata)
        with temporary_path.open("rb") as handle:
            os.fsync(handle.fileno())
        os.replace(temporary_path, destination_path)
        _fsync_directory(destination_path.parent)
    finally:
        temporary_path.unlink(missing_ok=True)
    return destination_path


def load_reference_affine(
    source: str | os.PathLike[str],
    *,
    config: ReferenceAffineConfig | None = None,
    device: str | torch.device = "cpu",
) -> ReferenceAffine:
    """Load exact affine state into a fresh object from a bounded safetensors file."""

    target_device = _validated_device(device)
    state = _validated_state(load_safetensors(_read_load_source(source)))
    if target_device != torch.device("cpu"):
        state = {
            name: tensor.to(device=target_device, non_blocking=False)
            for name, tensor in state.items()
        }
        state = _validated_state(state)

    model = ReferenceAffine(config)
    model.to(device=target_device)
    incompatible = model.load_state_dict(state, strict=True)
    if incompatible.missing_keys or incompatible.unexpected_keys:
        raise RuntimeError("strict reference affine state load unexpectedly reported key errors")
    model._validate_parameters()
    return model


def _validated_state(state: Mapping[str, torch.Tensor]) -> dict[str, torch.Tensor]:
    if set(state) != _STATE_KEYS:
        raise ValueError("reference affine state must contain exactly scale and bias")
    validated: dict[str, torch.Tensor] = {}
    for name in ("scale", "bias"):
        tensor = state[name]
        if not isinstance(tensor, torch.Tensor):
            raise TypeError(f"reference affine state {name} must be a torch.Tensor")
        if tensor.dtype != torch.float32:
            raise ValueError(f"reference affine state {name} must have dtype torch.float32")
        if tensor.shape != torch.Size([]):
            raise ValueError(f"reference affine state {name} must be scalar")
        if tensor.layout != torch.strided:
            raise ValueError(f"reference affine state {name} must use strided layout")
        if not torch.isfinite(tensor).item():
            raise ValueError(f"reference affine state {name} must be finite")
        validated[name] = tensor
    if validated["scale"].device != validated["bias"].device:
        raise ValueError("reference affine state tensors must be on the same device")
    return validated


def _validate_save_destination(destination: str | os.PathLike[str]) -> Path:
    path = Path(destination)
    if path.suffix != ".safetensors":
        raise ValueError("reference affine checkpoint must use a .safetensors suffix")
    if path.is_symlink():
        raise ValueError("reference affine checkpoint destination must not be a symlink")
    if not path.parent.is_dir():
        raise ValueError("reference affine checkpoint parent must be an existing directory")
    return path


def _read_load_source(source: str | os.PathLike[str]) -> bytes:
    path = Path(source)
    if path.suffix != ".safetensors":
        raise ValueError("reference affine checkpoint must use a .safetensors suffix")

    flags = os.O_RDONLY
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise ValueError("reference affine checkpoint cannot be opened safely") from error

    try:
        file_stat = os.fstat(descriptor)
        if not stat.S_ISREG(file_stat.st_mode):
            raise ValueError("reference affine checkpoint must be a regular file")
        if file_stat.st_size <= 0 or file_stat.st_size > MAXIMUM_REFERENCE_CHECKPOINT_BYTES:
            raise ValueError("reference affine checkpoint size is outside bounds")

        chunks: list[bytes] = []
        remaining = file_stat.st_size
        while remaining:
            chunk = os.read(descriptor, remaining)
            if not chunk:
                raise ValueError("reference affine checkpoint changed while being read")
            chunks.append(chunk)
            remaining -= len(chunk)
        if os.read(descriptor, 1):
            raise ValueError("reference affine checkpoint changed while being read")

        final_stat = os.fstat(descriptor)
        if (
            final_stat.st_size != file_stat.st_size
            or final_stat.st_mtime_ns != file_stat.st_mtime_ns
            or final_stat.st_ctime_ns != file_stat.st_ctime_ns
        ):
            raise ValueError("reference affine checkpoint changed while being read")
        return b"".join(chunks)
    finally:
        os.close(descriptor)


def _fsync_directory(directory: Path) -> None:
    flags = os.O_RDONLY
    if hasattr(os, "O_DIRECTORY"):
        flags |= os.O_DIRECTORY
    descriptor = os.open(directory, flags)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _validated_device(device: str | torch.device) -> torch.device:
    try:
        resolved = torch.device(device)
    except (TypeError, RuntimeError) as error:
        raise ValueError("reference affine device is invalid") from error
    if resolved.type == "meta":
        raise ValueError("reference affine checkpoints cannot be loaded onto the meta device")
    if resolved.type == "cuda" and not torch.cuda.is_available():
        raise ValueError("reference affine CUDA device requested but CUDA is unavailable")
    if resolved.type == "mps" and not torch.backends.mps.is_available():
        raise ValueError("reference affine MPS device requested but MPS is unavailable")
    return resolved
