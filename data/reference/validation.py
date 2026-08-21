# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reference snapshot integrity and compatibility gates."""

from __future__ import annotations

from data.manifest import ArtifactLocation

from .snapshot import ReferenceSnapshot


def validate_snapshot_locations(
    snapshot: ReferenceSnapshot, locations: dict[str, tuple[ArtifactLocation, ...]]
) -> None:
    expected = {artifact.digest for index in snapshot.indexes for artifact in index.artifacts}
    if set(locations) != expected:
        raise ValueError("reference artifact location coverage does not match the snapshot")
    for digest, values in locations.items():
        if not values or any(location.artifact_digest != digest for location in values):
            raise ValueError("reference artifact location is unbound or missing")


def require_compatible_tool(snapshot: ReferenceSnapshot, tool: str) -> None:
    if tool not in snapshot.compatible_search_tools:
        raise ValueError("search tool is incompatible with the reference snapshot")
