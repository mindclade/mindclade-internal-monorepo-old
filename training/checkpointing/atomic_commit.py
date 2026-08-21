# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Manifest-last atomic publication for a bounded local checkpoint directory."""

from __future__ import annotations

import os
import re
import shutil
import stat
import tempfile
from collections.abc import Mapping
from pathlib import Path
from typing import Final

from libs.python.errors import FailedPrecondition, InvalidArgument, ResourceExhausted

MANIFEST_PATH: Final = "manifest.json"
MAXIMUM_CHECKPOINT_FILES: Final = 16
MAXIMUM_CHECKPOINT_BYTES: Final = 512 << 20
_MEMBER_NAME = re.compile(r"[a-z][a-z0-9_.-]{0,254}")


def commit_checkpoint_directory(
    destination: Path,
    members: Mapping[str, bytes],
    manifest: bytes,
) -> Path:
    """Write members, then the commit manifest, and atomically publish the directory."""

    destination = _validated_destination(destination)
    if os.path.lexists(destination):
        raise FailedPrecondition(
            "checkpoint destination already exists",
            reason="checkpoint_destination_exists",
            fields={"path": str(destination)[:4096]},
        )
    if (
        not isinstance(members, Mapping)
        or not members
        or len(members) > MAXIMUM_CHECKPOINT_FILES - 1
        or MANIFEST_PATH in members
    ):
        raise InvalidArgument(
            "checkpoint member set is invalid or outside bounds",
            reason="checkpoint_members",
        )
    if not isinstance(manifest, bytes) or not manifest:
        raise InvalidArgument(
            "checkpoint manifest bytes are required",
            reason="checkpoint_manifest_bytes",
        )
    total = len(manifest)
    for name, value in members.items():
        if not isinstance(name, str) or _MEMBER_NAME.fullmatch(name) is None:
            raise InvalidArgument(
                "checkpoint member name is invalid",
                reason="checkpoint_member_name",
            )
        if not isinstance(value, bytes) or not value:
            raise InvalidArgument(
                "checkpoint members must contain non-empty bytes",
                reason="checkpoint_member_bytes",
            )
        total += len(value)
    if total > MAXIMUM_CHECKPOINT_BYTES:
        raise ResourceExhausted(
            "checkpoint exceeds the local byte bound",
            reason="checkpoint_size",
        )

    staging = Path(tempfile.mkdtemp(prefix=f".{destination.name}.staging-", dir=destination.parent))
    try:
        for name, value in sorted(members.items()):
            _write_durable(staging / name, value)
        _write_durable(staging / MANIFEST_PATH, manifest)
        _fsync_directory(staging)
        os.replace(staging, destination)
        _fsync_directory(destination.parent)
        return destination
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def read_checkpoint_member(root: Path, name: str, *, maximum_bytes: int) -> bytes:
    if not isinstance(name, str) or _MEMBER_NAME.fullmatch(name) is None:
        raise InvalidArgument(
            "checkpoint member name is invalid",
            reason="checkpoint_member_name",
        )
    if isinstance(maximum_bytes, bool) or not isinstance(maximum_bytes, int) or maximum_bytes <= 0:
        raise InvalidArgument(
            "checkpoint read bound must be positive",
            reason="checkpoint_read_bound",
        )
    path = root / name
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise InvalidArgument(
            f"checkpoint member cannot be opened safely: {name}",
            reason="checkpoint_member_open",
            cause=error,
        ) from error
    try:
        initial = os.fstat(descriptor)
        if not stat.S_ISREG(initial.st_mode) or not 0 < initial.st_size <= maximum_bytes:
            raise InvalidArgument(
                f"checkpoint member size or type is invalid: {name}",
                reason="checkpoint_member_stat",
            )
        chunks: list[bytes] = []
        remaining = initial.st_size
        while remaining:
            chunk = os.read(descriptor, min(1 << 20, remaining))
            if not chunk:
                raise InvalidArgument(
                    f"checkpoint member changed while reading: {name}",
                    reason="checkpoint_member_changed",
                )
            chunks.append(chunk)
            remaining -= len(chunk)
        if os.read(descriptor, 1):
            raise InvalidArgument(
                f"checkpoint member grew while reading: {name}",
                reason="checkpoint_member_changed",
            )
        final = os.fstat(descriptor)
        if (
            final.st_size != initial.st_size
            or final.st_mtime_ns != initial.st_mtime_ns
            or final.st_ctime_ns != initial.st_ctime_ns
        ):
            raise InvalidArgument(
                f"checkpoint member changed while reading: {name}",
                reason="checkpoint_member_changed",
            )
        return b"".join(chunks)
    finally:
        os.close(descriptor)


def validate_committed_root(root: Path, expected_members: set[str]) -> Path:
    if not isinstance(root, Path):
        raise InvalidArgument(
            "checkpoint root must be a Path",
            reason="checkpoint_root_type",
        )
    try:
        root_stat = root.lstat()
    except FileNotFoundError as error:
        raise InvalidArgument(
            "committed checkpoint directory does not exist",
            reason="checkpoint_root_missing",
            cause=error,
        ) from error
    if stat.S_ISLNK(root_stat.st_mode) or not stat.S_ISDIR(root_stat.st_mode):
        raise InvalidArgument(
            "checkpoint root must be a real directory",
            reason="checkpoint_root_type",
        )
    root = root.resolve()
    actual = {path.name for path in root.iterdir()}
    if actual != expected_members | {MANIFEST_PATH}:
        raise InvalidArgument(
            "checkpoint directory is incomplete or contains unexpected members",
            reason="checkpoint_root_members",
        )
    return root


def _validated_destination(destination: Path) -> Path:
    if not isinstance(destination, Path):
        raise InvalidArgument(
            "checkpoint destination must be a Path",
            reason="checkpoint_destination_type",
        )
    unresolved_parent = destination.parent
    try:
        parent_stat = unresolved_parent.lstat()
    except FileNotFoundError as error:
        raise InvalidArgument(
            "checkpoint destination parent does not exist",
            reason="checkpoint_destination_parent",
            cause=error,
        ) from error
    if stat.S_ISLNK(parent_stat.st_mode) or not stat.S_ISDIR(parent_stat.st_mode):
        raise InvalidArgument(
            "checkpoint destination parent must be a real directory",
            reason="checkpoint_destination_parent",
        )
    parent = unresolved_parent.resolve()
    if not destination.name or destination.name in {".", ".."}:
        raise InvalidArgument(
            "checkpoint destination name is invalid",
            reason="checkpoint_destination_name",
        )
    return parent / destination.name


def _write_durable(path: Path, value: bytes) -> None:
    with path.open("xb") as handle:
        handle.write(value)
        handle.flush()
        os.fsync(handle.fileno())
    path.chmod(0o600)


def _fsync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
