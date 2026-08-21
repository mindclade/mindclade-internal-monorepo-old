# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Narrow provider-neutral protocols for the Python data domain."""

from __future__ import annotations

from collections.abc import Iterable, Iterator
from typing import Protocol

from .manifest import ArtifactLocation, ArtifactRef
from .sample import Sample


class ArtifactVerifier(Protocol):
    """Verify bytes at a location without making storage authoritative."""

    def verify(self, artifact: ArtifactRef, location: ArtifactLocation) -> bool: ...


class SampleSource(Protocol):
    """Finite or explicitly resumable source of validated samples."""

    def iter_samples(self, *, cursor: str | None = None) -> Iterator[Sample]: ...

    def checkpoint(self) -> str: ...


class QualityValidator(Protocol):
    def validate(self, samples: Iterable[Sample]) -> object: ...


__all__ = ["ArtifactVerifier", "QualityValidator", "SampleSource"]
