# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded model-worker configuration."""

from __future__ import annotations

import json
import os
import stat
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from models.reference import DEFAULT_MAXIMUM_INPUT_ELEMENTS

MAX_CONFIG_BYTES = 64 * 1024
_DIGEST_LENGTH = len("sha256:") + 64


@dataclass(frozen=True, slots=True)
class ModelWorkerConfig:
    maximum_pending_requests: int = 1024
    maximum_batch_requests: int = 128
    maximum_gpu_bytes_per_batch: int = 80 * 1024**3

    def validate(self) -> None:
        if self.maximum_pending_requests <= 0:
            raise ValueError("pending-request limit must be positive")
        if (
            self.maximum_batch_requests <= 0
            or self.maximum_batch_requests > self.maximum_pending_requests
        ):
            raise ValueError("batch-request limit is invalid")
        if self.maximum_gpu_bytes_per_batch <= 0:
            raise ValueError("GPU batch budget must be positive")


@dataclass(frozen=True, slots=True)
class WorkerProcessConfig:
    """Fail-closed deployment configuration for the IPC worker process."""

    schema_version: int
    model_bundle_root: Path
    model_bundle_digest: str
    output_root: Path
    allowed_input_roots: tuple[Path, ...]
    device: str
    maximum_pending_requests: int
    maximum_concurrent_executions: int
    maximum_input_bytes: int
    maximum_output_bytes: int
    io_timeout_millis: int
    cancellation_grace_millis: int
    reference_chunk_elements: int
    reference_iterations: int

    @classmethod
    def from_file(cls, path: Path) -> WorkerProcessConfig:
        if not path.is_absolute():
            raise ValueError("worker config path must be absolute")
        encoded = _read_config_bytes(path)
        try:
            value = json.loads(
                encoded,
                object_pairs_hook=_unique_json_object,
                parse_constant=_reject_json_constant,
            )
        except (UnicodeDecodeError, json.JSONDecodeError, RecursionError, ValueError) as error:
            raise ValueError("worker config is not valid UTF-8 JSON") from error
        if not isinstance(value, dict):
            raise ValueError("worker config must be a JSON object")

        required = {
            "schema_version",
            "model_bundle_root",
            "model_bundle_digest",
            "output_root",
            "allowed_input_roots",
            "device",
            "maximum_pending_requests",
            "maximum_concurrent_executions",
            "maximum_input_bytes",
            "maximum_output_bytes",
            "io_timeout_millis",
            "cancellation_grace_millis",
            "reference_chunk_elements",
            "reference_iterations",
        }
        if set(value) != required:
            missing = sorted(required - set(value))
            unknown = sorted(set(value) - required)
            raise ValueError(
                f"worker config fields are invalid: missing={missing}, unknown={unknown}"
            )

        roots = value["allowed_input_roots"]
        if not isinstance(roots, list) or not roots or len(roots) > 32:
            raise ValueError("allowed_input_roots must contain 1 through 32 paths")
        config = cls(
            schema_version=_integer(value, "schema_version"),
            model_bundle_root=_path(value, "model_bundle_root"),
            model_bundle_digest=_string(value, "model_bundle_digest"),
            output_root=_path(value, "output_root"),
            allowed_input_roots=tuple(
                _absolute_path(item, "allowed_input_roots") for item in roots
            ),
            device=_string(value, "device"),
            maximum_pending_requests=_integer(value, "maximum_pending_requests"),
            maximum_concurrent_executions=_integer(value, "maximum_concurrent_executions"),
            maximum_input_bytes=_integer(value, "maximum_input_bytes"),
            maximum_output_bytes=_integer(value, "maximum_output_bytes"),
            io_timeout_millis=_integer(value, "io_timeout_millis"),
            cancellation_grace_millis=_integer(value, "cancellation_grace_millis"),
            reference_chunk_elements=_integer(value, "reference_chunk_elements"),
            reference_iterations=_integer(value, "reference_iterations"),
        )
        config.validate()
        return config

    def validate(self) -> None:
        if self.schema_version != 1:
            raise ValueError("unsupported worker config schema_version")
        if not _valid_digest(self.model_bundle_digest):
            raise ValueError("model_bundle_digest must be a sha256 digest")
        if self.device not in {"cpu", "cuda"}:
            raise ValueError("device must be cpu or cuda")
        _bounded(self.maximum_pending_requests, 1, 4096, "maximum_pending_requests")
        _bounded(
            self.maximum_concurrent_executions,
            1,
            self.maximum_pending_requests,
            "maximum_concurrent_executions",
        )
        _bounded(self.maximum_input_bytes, 4, 1 << 40, "maximum_input_bytes")
        _bounded(self.maximum_output_bytes, 4, 1 << 40, "maximum_output_bytes")
        _bounded(self.io_timeout_millis, 100, 300_000, "io_timeout_millis")
        _bounded(self.cancellation_grace_millis, 100, 30_000, "cancellation_grace_millis")
        _bounded(
            self.reference_chunk_elements,
            1,
            DEFAULT_MAXIMUM_INPUT_ELEMENTS,
            "reference_chunk_elements",
        )
        if self.reference_iterations != 1:
            raise ValueError(
                "reference.affine.v1 requires exactly one iteration (reference_iterations == 1)"
            )

        directories = (self.model_bundle_root, self.output_root, *self.allowed_input_roots)
        for directory in directories:
            if not directory.is_dir():
                raise ValueError(f"configured directory does not exist: {directory}")
            if directory.is_symlink() or directory.resolve(strict=True) != directory:
                raise ValueError(
                    f"configured directory must be canonical and not a symlink: {directory}"
                )
        if _contains(self.output_root, self.model_bundle_root):
            raise ValueError("output_root must not be inside model_bundle_root")


def _integer(value: dict[str, Any], field: str) -> int:
    candidate = value[field]
    if isinstance(candidate, bool) or not isinstance(candidate, int):
        raise ValueError(f"{field} must be an integer")
    return candidate


def _read_config_bytes(path: Path) -> bytes:
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise ValueError("worker config cannot be opened safely") from error
    try:
        initial = os.fstat(descriptor)
        if (
            not stat.S_ISREG(initial.st_mode)
            or initial.st_size <= 0
            or initial.st_size > MAX_CONFIG_BYTES
        ):
            raise ValueError("worker config size or type is outside bounds")
        chunks: list[bytes] = []
        remaining = initial.st_size
        while remaining:
            chunk = os.read(descriptor, min(remaining, 64 * 1024))
            if not chunk:
                raise ValueError("worker config changed while being read")
            chunks.append(chunk)
            remaining -= len(chunk)
        if os.read(descriptor, 1):
            raise ValueError("worker config changed while being read")
        final = os.fstat(descriptor)
        if (
            final.st_size != initial.st_size
            or final.st_mtime_ns != initial.st_mtime_ns
            or final.st_ctime_ns != initial.st_ctime_ns
        ):
            raise ValueError("worker config changed while being read")
        return b"".join(chunks)
    finally:
        os.close(descriptor)


def _unique_json_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    document: dict[str, Any] = {}
    for key, value in pairs:
        if key in document:
            raise ValueError(f"duplicate JSON key: {key}")
        document[key] = value
    return document


def _reject_json_constant(value: str) -> object:
    raise ValueError(f"non-finite JSON number: {value}")


def _string(value: dict[str, Any], field: str) -> str:
    candidate = value[field]
    if not isinstance(candidate, str) or candidate != candidate.strip() or not candidate:
        raise ValueError(f"{field} must be a non-empty trimmed string")
    return candidate


def _path(value: dict[str, Any], field: str) -> Path:
    return _absolute_path(_string(value, field), field)


def _absolute_path(value: object, field: str) -> Path:
    if not isinstance(value, str) or not value or value != value.strip():
        raise ValueError(f"{field} entries must be non-empty trimmed strings")
    path = Path(value)
    if not path.is_absolute():
        raise ValueError(f"{field} must contain absolute paths")
    return path


def _bounded(value: int, minimum: int, maximum: int, field: str) -> None:
    if value < minimum or value > maximum:
        raise ValueError(f"{field} is outside bounds [{minimum}, {maximum}]")


def _valid_digest(value: str) -> bool:
    return (
        len(value) == _DIGEST_LENGTH
        and value.startswith("sha256:")
        and all(character in "0123456789abcdef" for character in value[len("sha256:") :])
    )


def _contains(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
    except ValueError:
        return False
    return True
