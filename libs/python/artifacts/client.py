# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Injected, provider-neutral artifact reader with mandatory verification."""

from __future__ import annotations

from collections.abc import Callable, Iterable
from dataclasses import dataclass
from typing import Protocol

from libs.python.errors import InvalidArgument, ResourceExhausted
from libs.python.identifiers import ArtifactRef

from .reference import MAXIMUM_IN_MEMORY_ARTIFACT_BYTES
from .verification import verify_chunks


class ArtifactReader(Protocol):
    """A composition-root adapter that yields bytes for an authorized reference."""

    def read(self, reference: ArtifactRef) -> Iterable[bytes]: ...


@dataclass(frozen=True, slots=True)
class VerifiedArtifactClient:
    """Read bounded content and verify it before exposing bytes to Python code."""

    reader: ArtifactReader
    maximum_bytes: int = MAXIMUM_IN_MEMORY_ARTIFACT_BYTES

    def __post_init__(self) -> None:
        if not callable(getattr(self.reader, "read", None)):
            raise InvalidArgument(
                "artifact reader must implement read",
                reason="artifact_reader",
            )
        if (
            isinstance(self.maximum_bytes, bool)
            or not isinstance(self.maximum_bytes, int)
            or self.maximum_bytes < 0
        ):
            raise InvalidArgument(
                "artifact client byte bound must be a non-negative integer",
                reason="artifact_client_bound",
            )

    def read(
        self,
        reference: ArtifactRef,
        *,
        cancelled: Callable[[], bool] | None = None,
    ) -> bytes:
        if not isinstance(reference, ArtifactRef):
            raise InvalidArgument(
                "artifact read requires an ArtifactRef", reason="artifact_read_ref"
            )
        if reference.size_bytes > self.maximum_bytes:
            raise ResourceExhausted(
                f"artifact exceeds the {self.maximum_bytes}-byte client bound",
                reason="artifact_client_size",
            )
        captured: list[bytes] = []

        def recording_chunks() -> Iterable[bytes]:
            for chunk in self.reader.read(reference):
                if isinstance(chunk, bytes):
                    captured.append(chunk)
                yield chunk

        verify_chunks(reference, recording_chunks(), cancelled=cancelled)
        return b"".join(captured)
