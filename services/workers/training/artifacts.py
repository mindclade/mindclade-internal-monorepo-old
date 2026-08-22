# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Provider-neutral artifact boundary for the concrete training engine.

Rust and Go remain authoritative for execution grants, fencing, durable state, and provider
credentials.  This protocol is intentionally smaller than an object-store client: the Python
composition root may read an already-authorized immutable object, materialize an already-
verified checkpoint tree, and ask the supervising adapter to publish bytes or a tree under the
stage's authorized namespace.
"""

from __future__ import annotations

from collections.abc import Iterable
from pathlib import Path
from typing import Protocol, runtime_checkable

from libs.python.identifiers import ArtifactRef


@runtime_checkable
class ArtifactIO(Protocol):
    """Injected artifact operations available after runtime admission."""

    def read(self, reference: ArtifactRef) -> Iterable[bytes]:
        """Yield bytes for one authorized immutable reference."""

    def materialize_tree(self, reference: ArtifactRef) -> Path:
        """Return a local committed tree for an authorized manifest reference."""

    def publish_bytes(
        self,
        *,
        namespace: str,
        name: str,
        content: bytes,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        """Create an immutable byte artifact and return its canonical reference."""

    def publish_tree(
        self,
        *,
        namespace: str,
        name: str,
        source: Path,
        reference: ArtifactRef,
    ) -> ArtifactRef:
        """Create an immutable tree artifact and return its semantic manifest reference."""


__all__ = ["ArtifactIO"]
