# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Safe, deterministic PyTorch reference engine for runtime integration tests.

The reference model is intentionally small, but the loader and data boundaries are
production-shaped: only a verified safetensors bundle is accepted, inputs are bounded and
digest checked, execution is cancellation aware, and outputs are atomically published.
"""

from __future__ import annotations

import hashlib
import json
import os
import stat
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from threading import Event
from typing import Any

import torch
from safetensors.torch import load as load_safetensors

from models.reference import (
    DEFAULT_MAXIMUM_INPUT_ELEMENTS,
    MAXIMUM_REFERENCE_CHECKPOINT_BYTES,
    MAXIMUM_REFERENCE_CONFIG_BYTES,
    REFERENCE_AFFINE_MODEL_NAME,
    REFERENCE_AFFINE_OPERATION,
    ReferenceAffine,
    ReferenceAffineConfig,
    parse_reference_affine_config,
)

REFERENCE_MODEL_NAME = REFERENCE_AFFINE_MODEL_NAME
REFERENCE_OPERATION = REFERENCE_AFFINE_OPERATION
MANIFEST_MEDIA_TYPE = "application/vnd.mindclade.model.manifest.v1+json"
SAFETENSORS_MEDIA_TYPE = "application/vnd.mindclade.model.weights.v1+safetensors"
MAX_MANIFEST_BYTES = 1 << 20
MAX_MANIFEST_MEMBERS = 128


class ExecutionCancelled(RuntimeError):
    """Raised when cooperative cancellation or the signed deadline stops execution."""


@dataclass(frozen=True, slots=True)
class ReferenceInput:
    segment_id: str
    generation: int
    path: Path
    offset_bytes: int
    length_bytes: int
    element_type: str
    shape: tuple[int, ...]
    content_digest: str
    lease_expires_unix_millis: int


@dataclass(frozen=True, slots=True)
class ReferenceRequest:
    request_id: str
    model_bundle_digest: str
    operation: str
    deadline_unix_millis: int
    maximum_input_bytes: int
    maximum_output_bytes: int
    input: ReferenceInput


@dataclass(frozen=True, slots=True)
class ReferenceOutput:
    segment_id: str
    generation: int
    path: Path
    length_bytes: int
    element_type: str
    shape: tuple[int, ...]
    content_digest: str
    lease_expires_unix_millis: int


@dataclass(frozen=True, slots=True)
class ReferenceEngineConfig:
    model_bundle_root: Path
    expected_bundle_digest: str
    output_root: Path
    allowed_input_roots: tuple[Path, ...]
    device: str
    chunk_elements: int
    iterations: int


class ReferenceEngine:
    """Affine model ``y = scale * x + bias`` loaded from a verified bundle."""

    def __init__(self, config: ReferenceEngineConfig) -> None:
        self._config = config
        if (
            isinstance(config.chunk_elements, bool)
            or not isinstance(config.chunk_elements, int)
            or not 1 <= config.chunk_elements <= DEFAULT_MAXIMUM_INPUT_ELEMENTS
        ):
            raise ValueError("reference chunk_elements exceeds the model input budget")
        if (
            isinstance(config.iterations, bool)
            or not isinstance(config.iterations, int)
            or config.iterations != 1
        ):
            raise ValueError("reference.affine.v1 requires exactly one iteration")
        if config.device == "cuda" and not torch.cuda.is_available():
            raise RuntimeError("CUDA worker requested but torch reports no CUDA device")
        self._device = torch.device(config.device)
        weights, model_config = _load_verified_bundle(
            config.model_bundle_root, config.expected_bundle_digest
        )
        if config.chunk_elements > model_config.maximum_input_elements:
            raise ValueError("reference chunk_elements exceeds the bundle input budget")
        self._model = ReferenceAffine(model_config).to(self._device)
        try:
            incompatible = self._model.load_state_dict(weights, strict=True)
        except RuntimeError as error:
            raise ValueError(
                "reference bundle state is incompatible with the affine model"
            ) from error
        if incompatible.missing_keys or incompatible.unexpected_keys:
            raise ValueError("reference bundle must contain exactly scale and bias tensors")
        self._model.validate_state()
        self._model.eval()

    def execute(self, request: ReferenceRequest, cancelled: Event) -> ReferenceOutput:
        _validate_request(request, self._config, self._model.config.maximum_input_elements)
        _check_cancelled(cancelled, request.deadline_unix_millis)
        payload = _read_verified_input(request.input, request.maximum_input_bytes, self._config)
        values = torch.frombuffer(bytearray(payload), dtype=torch.float32)
        values = values.reshape(request.input.shape)
        if not torch.isfinite(values).all().item():
            raise ValueError("reference input must contain only finite values")

        chunks: list[torch.Tensor] = []
        with torch.inference_mode():
            flattened = values.reshape(-1)
            for start in range(0, flattened.numel(), self._config.chunk_elements):
                _check_cancelled(cancelled, request.deadline_unix_millis)
                chunk = flattened[start : start + self._config.chunk_elements].to(self._device)
                chunk = self._model.compute(chunk)
                if not torch.isfinite(chunk).all().item():
                    raise FloatingPointError(
                        "reference affine arithmetic produced non-finite output"
                    )
                chunks.append(chunk.to("cpu"))
        result = torch.cat(chunks).reshape(request.input.shape).contiguous().numpy().tobytes()
        if len(result) > request.maximum_output_bytes:
            raise ValueError("reference output exceeds signed output budget")
        return _publish_output(request, result, self._config.output_root)


def _load_verified_bundle(
    root: Path, expected_digest: str
) -> tuple[dict[str, torch.Tensor], ReferenceAffineConfig]:
    if (
        not root.is_absolute()
        or root.is_symlink()
        or root.resolve(strict=True) != root
        or not root.is_dir()
    ):
        raise ValueError("reference bundle root must be a canonical directory")
    manifest_path = root / "manifest.json"
    manifest_bytes = _read_regular_file(manifest_path, maximum_bytes=MAX_MANIFEST_BYTES)
    try:
        manifest = json.loads(
            manifest_bytes,
            object_pairs_hook=_unique_json_object,
            parse_constant=_reject_json_constant,
        )
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
        raise ValueError("reference manifest is not valid UTF-8 JSON") from error
    if not isinstance(manifest, dict):
        raise ValueError("reference manifest must be a JSON object")
    required = {
        "schema_version",
        "media_type",
        "logical_kind",
        "name",
        "digest",
        "size_bytes",
        "members",
    }
    if set(manifest) != required:
        raise ValueError("reference manifest fields do not match schema v1")
    if (
        isinstance(manifest["schema_version"], bool)
        or not isinstance(manifest["schema_version"], int)
        or manifest["schema_version"] != 1
        or manifest["media_type"] != MANIFEST_MEDIA_TYPE
        or manifest["logical_kind"] != "model.bundle"
        or manifest["name"] != REFERENCE_MODEL_NAME
        or manifest["digest"] != expected_digest
        or isinstance(manifest["size_bytes"], bool)
        or not isinstance(manifest["size_bytes"], int)
        or manifest["size_bytes"] <= 0
    ):
        raise ValueError("reference manifest identity does not match worker configuration")
    members = manifest["members"]
    if not isinstance(members, list) or len(members) != 2:
        raise ValueError("reference manifest member count is outside bounds")

    verified_members: list[dict[str, Any]] = []
    member_payloads: dict[str, bytes] = {}
    total_size = 0
    for member in members:
        if not isinstance(member, dict):
            raise ValueError("reference manifest member must be an object")
        member_required = {
            "path",
            "digest",
            "size_bytes",
            "media_type",
            "logical_kind",
            "schema_version",
        }
        if (
            set(member) != member_required
            or isinstance(member["schema_version"], bool)
            or not isinstance(member["schema_version"], int)
            or member["schema_version"] != 1
            or isinstance(member["size_bytes"], bool)
            or not isinstance(member["size_bytes"], int)
            or member["size_bytes"] <= 0
        ):
            raise ValueError("reference manifest member fields do not match schema v1")
        relative = member["path"]
        if (
            not isinstance(relative, str)
            or not relative
            or Path(relative).is_absolute()
            or ".." in Path(relative).parts
        ):
            raise ValueError("reference manifest member path is unsafe")
        path = root / relative
        maximum_bytes = (
            MAXIMUM_REFERENCE_CHECKPOINT_BYTES
            if relative == "model.safetensors"
            else MAXIMUM_REFERENCE_CONFIG_BYTES
            if relative == "config.json"
            else 0
        )
        if maximum_bytes == 0:
            raise ValueError("reference bundle contains an unsupported member")
        if relative in member_payloads:
            raise ValueError("reference bundle contains a duplicate member")
        payload = _read_regular_file(path, maximum_bytes=maximum_bytes)
        if len(payload) != member["size_bytes"] or _sha256_bytes(payload) != member["digest"]:
            raise ValueError(f"reference bundle member failed verification: {relative}")
        total_size += len(payload)
        verified_members.append(member)
        member_payloads[relative] = payload

    expected_member_contracts = {
        "config.json": ("application/json", "model.metadata"),
        "model.safetensors": (SAFETENSORS_MEDIA_TYPE, "model.weights"),
    }
    if set(member_payloads) != set(expected_member_contracts):
        raise ValueError("reference bundle members do not match the affine v1 contract")
    for member in verified_members:
        media_type, logical_kind = expected_member_contracts[member["path"]]
        if member["media_type"] != media_type or member["logical_kind"] != logical_kind:
            raise ValueError("reference bundle member has inconsistent metadata")

    canonical = json.dumps(verified_members, sort_keys=True, separators=(",", ":")).encode()
    actual_digest = "sha256:" + hashlib.sha256(canonical).hexdigest()
    if actual_digest != expected_digest or total_size != manifest["size_bytes"]:
        raise ValueError("reference bundle aggregate identity failed verification")
    try:
        weights = load_safetensors(member_payloads["model.safetensors"])
    except Exception as error:
        raise ValueError("reference bundle weights are not valid safetensors") from error
    model_config = parse_reference_affine_config(member_payloads["config.json"])
    return weights, model_config


def _validate_request(
    request: ReferenceRequest,
    config: ReferenceEngineConfig,
    maximum_input_elements: int,
) -> None:
    if request.operation != REFERENCE_OPERATION:
        raise ValueError("unsupported reference worker operation")
    if request.model_bundle_digest != config.expected_bundle_digest:
        raise ValueError("ticket model digest does not match loaded bundle")
    if not request.request_id or len(request.request_id) > 256:
        raise ValueError("reference request id is outside bounds")
    if request.maximum_output_bytes <= 0:
        raise ValueError("signed output budget must be positive")
    input_descriptor = request.input
    if input_descriptor.element_type not in {"f32", "float32"}:
        raise ValueError("reference input element type must be f32")
    if (
        not input_descriptor.shape
        or len(input_descriptor.shape) > 16
        or any(dimension <= 0 for dimension in input_descriptor.shape)
    ):
        raise ValueError("reference input shape is outside bounds")
    elements = 1
    for dimension in input_descriptor.shape:
        elements *= dimension
        if elements > request.maximum_input_bytes // 4:
            raise ValueError("reference input shape exceeds input budget")
        if elements > maximum_input_elements:
            raise ValueError("reference input shape exceeds the bundle input budget")
    if elements * 4 != input_descriptor.length_bytes:
        raise ValueError("reference input shape does not match byte length")


def _read_verified_input(
    descriptor: ReferenceInput,
    maximum_input_bytes: int,
    config: ReferenceEngineConfig,
) -> bytes:
    if descriptor.length_bytes <= 0 or descriptor.length_bytes > maximum_input_bytes:
        raise ValueError("reference input byte length is outside bounds")
    if descriptor.offset_bytes < 0:
        raise ValueError("reference input offset is invalid")
    path = descriptor.path
    if not path.is_absolute() or path.is_symlink() or path.resolve(strict=True) != path:
        raise ValueError("reference input path must be canonical and not a symlink")
    if not any(_contains(path, root) for root in config.allowed_input_roots):
        raise ValueError("reference input path is outside configured roots")
    file_stat = _regular_file(path)
    if file_stat.st_uid != os.geteuid():
        raise ValueError("reference input file must be owned by the worker uid")
    end = descriptor.offset_bytes + descriptor.length_bytes
    if end > file_stat.st_size:
        raise ValueError("reference input range exceeds file length")
    flags = os.O_RDONLY
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    descriptor_fd = os.open(path, flags)
    try:
        payload = os.pread(descriptor_fd, descriptor.length_bytes, descriptor.offset_bytes)
    finally:
        os.close(descriptor_fd)
    if (
        len(payload) != descriptor.length_bytes
        or _sha256_bytes(payload) != descriptor.content_digest
    ):
        raise ValueError("reference input content digest mismatch")
    return payload


def _publish_output(
    request: ReferenceRequest, payload: bytes, output_root: Path
) -> ReferenceOutput:
    stem = hashlib.sha256(request.request_id.encode()).hexdigest()
    target = output_root / f"{stem}.f32"
    descriptor_fd, temporary_name = tempfile.mkstemp(prefix=f".{stem}.", dir=output_root)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor_fd, "wb") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, stat.S_IRUSR)
        os.replace(temporary, target)
    except BaseException:
        temporary.unlink(missing_ok=True)
        raise
    return ReferenceOutput(
        segment_id=f"reference-output-{stem[:32]}",
        generation=request.input.generation + 1,
        path=target,
        length_bytes=len(payload),
        element_type="f32",
        shape=request.input.shape,
        content_digest=_sha256_bytes(payload),
        lease_expires_unix_millis=request.deadline_unix_millis,
    )


def _regular_file(path: Path) -> os.stat_result:
    if path.is_symlink() or path.resolve(strict=True) != path:
        raise ValueError(f"path must be canonical and not a symlink: {path}")
    result = path.stat()
    if not stat.S_ISREG(result.st_mode):
        raise ValueError(f"path must be a regular file: {path}")
    return result


def _read_regular_file(path: Path, *, maximum_bytes: int) -> bytes:
    flags = os.O_RDONLY
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise ValueError(f"path cannot be opened safely: {path}") from error
    try:
        initial = os.fstat(descriptor)
        if not stat.S_ISREG(initial.st_mode):
            raise ValueError(f"path must be a regular file: {path}")
        if initial.st_size <= 0 or initial.st_size > maximum_bytes:
            raise ValueError(f"file size is outside bounds: {path}")
        chunks: list[bytes] = []
        remaining = initial.st_size
        while remaining:
            chunk = os.read(descriptor, min(remaining, 1 << 20))
            if not chunk:
                raise ValueError(f"file changed while being read: {path}")
            chunks.append(chunk)
            remaining -= len(chunk)
        if os.read(descriptor, 1):
            raise ValueError(f"file changed while being read: {path}")
        final = os.fstat(descriptor)
        if (
            final.st_size != initial.st_size
            or final.st_mtime_ns != initial.st_mtime_ns
            or final.st_ctime_ns != initial.st_ctime_ns
        ):
            raise ValueError(f"file changed while being read: {path}")
        return b"".join(chunks)
    finally:
        os.close(descriptor)


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(1 << 20):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def _sha256_bytes(value: bytes) -> str:
    return "sha256:" + hashlib.sha256(value).hexdigest()


def _unique_json_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def _reject_json_constant(value: str) -> object:
    raise ValueError(f"non-finite JSON number: {value}")


def _contains(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
    except ValueError:
        return False
    return True


def _check_cancelled(cancelled: Event, deadline_unix_millis: int) -> None:
    if cancelled.is_set():
        raise ExecutionCancelled("reference execution cancelled")
    if int(time.time() * 1000) >= deadline_unix_millis:
        raise ExecutionCancelled("reference execution deadline exceeded")
