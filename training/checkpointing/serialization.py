# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pickle-free serialization of model, optimizer, and RNG state."""

from __future__ import annotations

import json
import math
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Final, cast

import torch
from safetensors.torch import load as load_safetensors
from safetensors.torch import save as save_safetensors

from libs.python.errors import InvalidArgument, ResourceExhausted
from libs.python.serialization import canonical_json_bytes

STATE_SCHEMA_VERSION: Final = 1
MAXIMUM_TREE_DEPTH: Final = 64
MAXIMUM_TREE_NODES: Final = 1_000_000
MAXIMUM_TENSORS: Final = 100_000
MAXIMUM_TENSOR_BYTES: Final = 256 << 20
MAXIMUM_STRING_BYTES: Final = 1 << 20
MAXIMUM_INTEGER: Final = (1 << 63) - 1


@dataclass(frozen=True, slots=True)
class DecodedTrainingState:
    model: Mapping[str, object]
    optimizer: Mapping[str, object]
    torch_rng: torch.Tensor


class _Encoder:
    def __init__(self) -> None:
        self.nodes = 0
        self.tensor_bytes = 0
        self.tensors: dict[str, torch.Tensor] = {}

    def encode(self, value: object, *, depth: int = 0) -> dict[str, object]:
        self.nodes += 1
        if self.nodes > MAXIMUM_TREE_NODES:
            raise ResourceExhausted(
                "checkpoint state tree exceeds its node bound",
                reason="checkpoint_state_nodes",
            )
        if depth > MAXIMUM_TREE_DEPTH:
            raise ResourceExhausted(
                "checkpoint state tree exceeds its depth bound",
                reason="checkpoint_state_depth",
            )
        if isinstance(value, torch.Tensor):
            return self._tensor(value)
        if value is None:
            return {"type": "none"}
        if isinstance(value, bool):
            return {"type": "bool", "value": value}
        if isinstance(value, int):
            if not -MAXIMUM_INTEGER <= value <= MAXIMUM_INTEGER:
                raise InvalidArgument(
                    "checkpoint integer is outside signed 64-bit bounds",
                    reason="checkpoint_state_integer",
                )
            return {"type": "int", "value": value}
        if isinstance(value, float):
            if not math.isfinite(value):
                raise FloatingPointError("checkpoint scalar state is not finite")
            return {"type": "float", "value": value.hex()}
        if isinstance(value, str):
            if len(value.encode("utf-8")) > MAXIMUM_STRING_BYTES:
                raise ResourceExhausted(
                    "checkpoint string exceeds its byte bound",
                    reason="checkpoint_state_string",
                )
            return {"type": "str", "value": value}
        if isinstance(value, Mapping):
            items = [
                (self.encode(key, depth=depth + 1), self.encode(item, depth=depth + 1))
                for key, item in value.items()
            ]
            for key, _ in items:
                if key.get("type") not in {"int", "str"}:
                    raise InvalidArgument(
                        "checkpoint mapping keys must be strings or integers",
                        reason="checkpoint_state_key",
                    )
            items.sort(key=lambda pair: canonical_json_bytes(pair[0]))
            return {"type": "dict", "value": [[key, item] for key, item in items]}
        if isinstance(value, tuple):
            return {
                "type": "tuple",
                "value": [self.encode(item, depth=depth + 1) for item in value],
            }
        if isinstance(value, list):
            return {
                "type": "list",
                "value": [self.encode(item, depth=depth + 1) for item in value],
            }
        raise InvalidArgument(
            f"checkpoint state type is not safely serializable: {type(value).__name__}",
            reason="checkpoint_state_type",
        )

    def _tensor(self, value: torch.Tensor) -> dict[str, object]:
        if len(self.tensors) >= MAXIMUM_TENSORS:
            raise ResourceExhausted(
                "checkpoint tensor count exceeds its bound",
                reason="checkpoint_state_tensor_count",
            )
        if value.layout is not torch.strided or value.is_quantized or value.is_complex():
            raise InvalidArgument(
                "reference checkpoints require dense non-complex strided tensors",
                reason="checkpoint_state_tensor_layout",
            )
        if value.is_floating_point() and not bool(torch.isfinite(value.detach()).all().item()):
            raise FloatingPointError("checkpoint tensor state is not finite")
        size = value.numel() * value.element_size()
        self.tensor_bytes += size
        if self.tensor_bytes > MAXIMUM_TENSOR_BYTES:
            raise ResourceExhausted(
                "checkpoint tensor bytes exceed the local reference bound",
                reason="checkpoint_state_tensor_bytes",
            )
        name = f"tensor-{len(self.tensors):08d}"
        self.tensors[name] = value.detach().to(device="cpu").contiguous().clone()
        return {"type": "tensor", "value": name}


class _Decoder:
    def __init__(self, tensors: Mapping[str, torch.Tensor]) -> None:
        self.tensors = tensors
        self.used: set[str] = set()
        self.nodes = 0

    def decode(self, value: object, *, depth: int = 0) -> object:
        self.nodes += 1
        if self.nodes > MAXIMUM_TREE_NODES or depth > MAXIMUM_TREE_DEPTH:
            raise ResourceExhausted(
                "checkpoint state tree exceeds decode bounds",
                reason="checkpoint_state_decode_bound",
            )
        if not isinstance(value, dict) or not isinstance(value.get("type"), str):
            raise InvalidArgument(
                "checkpoint state node is invalid",
                reason="checkpoint_state_node",
            )
        kind = value["type"]
        expected = {"type"} if kind == "none" else {"type", "value"}
        if set(value) != expected:
            raise InvalidArgument(
                "checkpoint state node fields are invalid",
                reason="checkpoint_state_node",
            )
        payload = value.get("value")
        if kind == "none":
            return None
        if kind == "bool" and isinstance(payload, bool):
            return payload
        if (
            kind == "int"
            and isinstance(payload, int)
            and not isinstance(payload, bool)
            and -MAXIMUM_INTEGER <= payload <= MAXIMUM_INTEGER
        ):
            return payload
        if kind == "float" and isinstance(payload, str):
            try:
                decoded_float = float.fromhex(payload)
            except (OverflowError, ValueError) as error:
                raise InvalidArgument(
                    "checkpoint float encoding is invalid",
                    reason="checkpoint_state_float",
                    cause=error,
                ) from error
            if math.isfinite(decoded_float):
                return decoded_float
        if (
            kind == "str"
            and isinstance(payload, str)
            and len(payload.encode("utf-8")) <= MAXIMUM_STRING_BYTES
        ):
            return payload
        if kind == "tensor" and isinstance(payload, str):
            if payload in self.used or payload not in self.tensors:
                raise InvalidArgument(
                    "checkpoint tensor reference is missing or duplicated",
                    reason="checkpoint_state_tensor_reference",
                )
            self.used.add(payload)
            return self.tensors[payload].clone()
        if kind in {"list", "tuple"} and isinstance(payload, list):
            decoded_items = [self.decode(item, depth=depth + 1) for item in payload]
            return tuple(decoded_items) if kind == "tuple" else decoded_items
        if kind == "dict" and isinstance(payload, list):
            decoded_mapping: dict[object, object] = {}
            for pair in payload:
                if not isinstance(pair, list) or len(pair) != 2:
                    break
                key = self.decode(pair[0], depth=depth + 1)
                if (
                    not isinstance(key, str | int)
                    or isinstance(key, bool)
                    or key in decoded_mapping
                ):
                    raise InvalidArgument(
                        "checkpoint mapping key is invalid or duplicated",
                        reason="checkpoint_state_key",
                    )
                decoded_mapping[key] = self.decode(pair[1], depth=depth + 1)
            else:
                return decoded_mapping
        raise InvalidArgument(
            f"checkpoint state node {kind!r} is invalid",
            reason="checkpoint_state_node",
        )


def encode_training_state(
    model: Mapping[str, object],
    optimizer: Mapping[str, object],
    torch_rng: torch.Tensor,
) -> tuple[bytes, bytes]:
    _validate_torch_rng_state(torch_rng)
    encoder = _Encoder()
    document = {
        "schema_version": STATE_SCHEMA_VERSION,
        "model": encoder.encode(model),
        "optimizer": encoder.encode(optimizer),
        "torch_rng": encoder.encode(torch_rng),
    }
    metadata = canonical_json_bytes(document)
    try:
        tensor_bytes = save_safetensors(encoder.tensors)
    except (RuntimeError, ValueError) as error:
        raise InvalidArgument(
            "checkpoint tensors cannot be encoded as safetensors",
            reason="checkpoint_state_safetensors",
            cause=error,
        ) from error
    if len(tensor_bytes) > MAXIMUM_TENSOR_BYTES + (100 << 20):
        raise ResourceExhausted(
            "encoded checkpoint tensor archive exceeds bounds",
            reason="checkpoint_state_tensor_bytes",
        )
    return metadata, tensor_bytes


def decode_training_state(metadata: bytes, tensor_bytes: bytes) -> DecodedTrainingState:
    try:
        document = json.loads(metadata, object_pairs_hook=_unique_object)
    except (UnicodeDecodeError, json.JSONDecodeError, ValueError) as error:
        raise InvalidArgument(
            "checkpoint state metadata is not unique-key UTF-8 JSON",
            reason="checkpoint_state_json",
            cause=error,
        ) from error
    if not isinstance(document, dict) or set(document) != {
        "schema_version",
        "model",
        "optimizer",
        "torch_rng",
    }:
        raise InvalidArgument(
            "checkpoint state metadata fields do not match schema v1",
            reason="checkpoint_state_fields",
        )
    if document["schema_version"] != STATE_SCHEMA_VERSION:
        raise InvalidArgument(
            "checkpoint state schema version is unsupported",
            reason="checkpoint_state_version",
        )
    try:
        tensors = load_safetensors(tensor_bytes)
    except (RuntimeError, ValueError) as error:
        raise InvalidArgument(
            "checkpoint tensor archive is invalid",
            reason="checkpoint_state_safetensors",
            cause=error,
        ) from error
    if len(tensors) > MAXIMUM_TENSORS:
        raise ResourceExhausted(
            "checkpoint tensor count exceeds its bound",
            reason="checkpoint_state_tensor_count",
        )
    tensor_size = 0
    for tensor in tensors.values():
        if tensor.layout is not torch.strided or tensor.is_quantized or tensor.is_complex():
            raise InvalidArgument(
                "checkpoint tensor archive contains an unsupported tensor",
                reason="checkpoint_state_tensor_layout",
            )
        tensor_size += tensor.numel() * tensor.element_size()
        if tensor_size > MAXIMUM_TENSOR_BYTES:
            raise ResourceExhausted(
                "checkpoint tensor bytes exceed the local reference bound",
                reason="checkpoint_state_tensor_bytes",
            )
        if tensor.is_floating_point() and not bool(torch.isfinite(tensor).all().item()):
            raise FloatingPointError("checkpoint tensor state is not finite")
    decoder = _Decoder(tensors)
    model = decoder.decode(document["model"])
    optimizer = decoder.decode(document["optimizer"])
    torch_rng = decoder.decode(document["torch_rng"])
    if decoder.used != set(tensors):
        raise InvalidArgument(
            "checkpoint tensor archive contains unreferenced tensors",
            reason="checkpoint_state_tensor_reference",
        )
    if not isinstance(model, dict) or not all(isinstance(key, str) for key in model):
        raise InvalidArgument(
            "checkpoint model state must be a string-keyed mapping",
            reason="checkpoint_model_state",
        )
    if not isinstance(optimizer, dict) or not all(isinstance(key, str) for key in optimizer):
        raise InvalidArgument(
            "checkpoint optimizer state must be a string-keyed mapping",
            reason="checkpoint_optimizer_state",
        )
    if (
        not isinstance(torch_rng, torch.Tensor)
        or torch_rng.device.type != "cpu"
        or torch_rng.dtype is not torch.uint8
        or torch_rng.ndim != 1
    ):
        raise InvalidArgument(
            "checkpoint Torch RNG state is invalid",
            reason="checkpoint_rng_state",
        )
    _validate_torch_rng_state(torch_rng)
    return DecodedTrainingState(
        cast(dict[str, object], model),
        cast(dict[str, object], optimizer),
        torch_rng,
    )


def _validate_torch_rng_state(value: torch.Tensor) -> None:
    if (
        not isinstance(value, torch.Tensor)
        or value.device.type != "cpu"
        or value.dtype is not torch.uint8
        or value.ndim != 1
    ):
        raise InvalidArgument(
            "checkpoint Torch RNG state is invalid",
            reason="checkpoint_rng_state",
        )
    try:
        generator = torch.Generator(device="cpu")
        generator.set_state(value)
    except (RuntimeError, ValueError) as error:
        raise InvalidArgument(
            "checkpoint Torch RNG state is incompatible with this toolchain",
            reason="checkpoint_rng_state",
            cause=error,
        ) from error


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, item in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key {key!r}")
        result[key] = item
    return result
