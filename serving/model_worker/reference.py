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
from safetensors.torch import load_file

REFERENCE_MODEL_NAME = "reference-affine-v1"
REFERENCE_OPERATION = "reference.affine.v1"
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
        if config.device == "cuda" and not torch.cuda.is_available():
            raise RuntimeError("CUDA worker requested but torch reports no CUDA device")
        self._device = torch.device(config.device)
        weights = _load_verified_bundle(config.model_bundle_root, config.expected_bundle_digest)
        try:
            scale = weights["scale"]
            bias = weights["bias"]
        except KeyError as error:
            raise ValueError("reference bundle must contain scale and bias tensors") from error
        if scale.dtype != torch.float32 or bias.dtype != torch.float32:
            raise ValueError("reference scale and bias must be float32")
        if scale.numel() != 1 or bias.numel() != 1:
            raise ValueError("reference scale and bias must each contain one value")
        self._scale = scale.reshape(()).to(self._device)
        self._bias = bias.reshape(()).to(self._device)

    def execute(self, request: ReferenceRequest, cancelled: Event) -> ReferenceOutput:
        _validate_request(request, self._config)
        _check_cancelled(cancelled, request.deadline_unix_millis)
        payload = _read_verified_input(request.input, request.maximum_input_bytes, self._config)
        values = torch.frombuffer(bytearray(payload), dtype=torch.float32)
        values = values.reshape(request.input.shape)

        chunks: list[torch.Tensor] = []
        with torch.inference_mode():
            flattened = values.reshape(-1)
            for start in range(0, flattened.numel(), self._config.chunk_elements):
                _check_cancelled(cancelled, request.deadline_unix_millis)
                chunk = flattened[start : start + self._config.chunk_elements].to(self._device)
                for _ in range(self._config.iterations):
                    _check_cancelled(cancelled, request.deadline_unix_millis)
                    chunk = torch.add(torch.mul(chunk, self._scale), self._bias)
                chunks.append(chunk.to("cpu"))
        result = torch.cat(chunks).reshape(request.input.shape).contiguous().numpy().tobytes()
        if len(result) > request.maximum_output_bytes:
            raise ValueError("reference output exceeds signed output budget")
        return _publish_output(request, result, self._config.output_root)


def _load_verified_bundle(root: Path, expected_digest: str) -> dict[str, torch.Tensor]:
    manifest_path = root / "manifest.json"
    manifest_stat = _regular_file(manifest_path)
    if manifest_stat.st_size == 0 or manifest_stat.st_size > MAX_MANIFEST_BYTES:
        raise ValueError("reference manifest size is outside bounds")
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
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
        manifest["schema_version"] != 1
        or manifest["media_type"] != MANIFEST_MEDIA_TYPE
        or manifest["logical_kind"] != "model.bundle"
        or manifest["name"] != REFERENCE_MODEL_NAME
        or manifest["digest"] != expected_digest
    ):
        raise ValueError("reference manifest identity does not match worker configuration")
    members = manifest["members"]
    if not isinstance(members, list) or not members or len(members) > MAX_MANIFEST_MEMBERS:
        raise ValueError("reference manifest member count is outside bounds")

    verified_members: list[dict[str, Any]] = []
    weight_paths: list[Path] = []
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
        if set(member) != member_required or member["schema_version"] != 1:
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
        file_stat = _regular_file(path)
        if file_stat.st_size != member["size_bytes"] or _sha256_file(path) != member["digest"]:
            raise ValueError(f"reference bundle member failed verification: {relative}")
        total_size += file_stat.st_size
        verified_members.append(member)
        if member["media_type"] == SAFETENSORS_MEDIA_TYPE:
            if path.suffix != ".safetensors" or member["logical_kind"] != "model.weights":
                raise ValueError("reference weights member has inconsistent metadata")
            weight_paths.append(path)

    canonical = json.dumps(verified_members, sort_keys=True, separators=(",", ":")).encode()
    actual_digest = "sha256:" + hashlib.sha256(canonical).hexdigest()
    if actual_digest != expected_digest or total_size != manifest["size_bytes"]:
        raise ValueError("reference bundle aggregate identity failed verification")
    if len(weight_paths) != 1:
        raise ValueError("reference bundle must contain exactly one safetensors weights file")
    return load_file(weight_paths[0], device="cpu")


def _validate_request(request: ReferenceRequest, config: ReferenceEngineConfig) -> None:
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


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(1 << 20):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def _sha256_bytes(value: bytes) -> str:
    return "sha256:" + hashlib.sha256(value).hexdigest()


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
