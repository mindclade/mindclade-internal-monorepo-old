# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded framed records received from the Rust artifact plane."""

from __future__ import annotations

import hashlib
import re
from dataclasses import dataclass, field

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
MAX_PAYLOAD_BYTES = 64 * 1024 * 1024
MAX_METADATA = 64


@dataclass(frozen=True, slots=True)
class RawRecord:
    key: str
    payload: bytes
    source_snapshot_digest: str
    offset: int
    metadata: tuple[tuple[str, str], ...] = field(default_factory=tuple)

    def __post_init__(self) -> None:
        if (
            not isinstance(self.key, str)
            or not self.key
            or len(self.key.encode("utf-8")) > 1024
            or any(ord(character) < 0x20 for character in self.key)
        ):
            raise ValueError("raw record key is invalid")
        if not isinstance(self.payload, bytes) or not 1 <= len(self.payload) <= MAX_PAYLOAD_BYTES:
            raise ValueError("raw record payload is outside bounds")
        if not isinstance(self.source_snapshot_digest, str) or not _DIGEST.fullmatch(
            self.source_snapshot_digest
        ):
            raise ValueError("raw record source snapshot digest is invalid")
        if isinstance(self.offset, bool) or not isinstance(self.offset, int) or self.offset < 0:
            raise ValueError("raw record offset is invalid")
        metadata = tuple(self.metadata)
        if len(metadata) > MAX_METADATA:
            raise ValueError("raw record metadata exceeds bounds")
        previous = ""
        for key, value in metadata:
            if (
                not isinstance(key, str)
                or not isinstance(value, str)
                or not key
                or key <= previous
                or not value
                or len(key) > 128
                or len(value) > 4096
            ):
                raise ValueError("raw record metadata must be sorted, unique, and bounded")
            previous = key
        object.__setattr__(self, "metadata", metadata)

    @property
    def digest(self) -> str:
        digest = hashlib.sha256()
        digest.update(self.source_snapshot_digest.encode("ascii"))
        digest.update(b"\0")
        digest.update(str(self.offset).encode("ascii"))
        digest.update(b"\0")
        digest.update(self.key.encode("utf-8"))
        digest.update(b"\0")
        digest.update(self.payload)
        for key, value in self.metadata:
            digest.update(b"\0" + key.encode("utf-8") + b"=" + value.encode("utf-8"))
        return "sha256:" + digest.hexdigest()
