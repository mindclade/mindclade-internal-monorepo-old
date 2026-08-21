# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Read-only exact-version reference catalog."""

from __future__ import annotations

from collections.abc import Iterable

from .snapshot import ReferenceSnapshot


class ReferenceCatalog:
    def __init__(self, snapshots: Iterable[ReferenceSnapshot]) -> None:
        entries: dict[tuple[str, str], ReferenceSnapshot] = {}
        for snapshot in snapshots:
            if not isinstance(snapshot, ReferenceSnapshot):
                raise TypeError("reference catalog entries must be ReferenceSnapshot values")
            key = (snapshot.reference_id, snapshot.version)
            if key in entries:
                raise ValueError("reference catalog identity/version must be unique")
            entries[key] = snapshot
        self._entries = entries

    def resolve(self, reference_id: str, version: str) -> ReferenceSnapshot:
        try:
            return self._entries[(reference_id, version)]
        except KeyError as error:
            raise KeyError(f"unknown exact reference snapshot {reference_id}@{version}") from error
