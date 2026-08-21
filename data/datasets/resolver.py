# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Resolve an exact dataset manifest to integrity-bound artifact locations."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from data.manifest import ArtifactLocation, ArtifactRef

from .catalog import DatasetCatalog
from .versioning import DatasetVersionManifest


class LocationCatalog(Protocol):
    def locations(self, artifact: ArtifactRef) -> tuple[ArtifactLocation, ...]: ...


@dataclass(frozen=True, slots=True)
class ResolvedArtifact:
    artifact: ArtifactRef
    locations: tuple[ArtifactLocation, ...]


@dataclass(frozen=True, slots=True)
class ResolvedDataset:
    manifest: DatasetVersionManifest
    artifacts: tuple[ResolvedArtifact, ...]


class DatasetResolver:
    def __init__(self, catalog: DatasetCatalog, locations: LocationCatalog) -> None:
        self._catalog = catalog
        self._locations = locations

    def resolve(self, dataset_id: str, version: str) -> ResolvedDataset:
        manifest = self._catalog.resolve(dataset_id, version)
        resolved: list[ResolvedArtifact] = []
        for artifact in manifest.artifacts:
            locations = tuple(self._locations.locations(artifact))
            if not locations:
                raise LookupError(f"artifact has no registered location: {artifact.digest}")
            if any(not isinstance(location, ArtifactLocation) for location in locations):
                raise TypeError("location catalog returned an invalid location")
            if any(not location.binds(artifact) for location in locations):
                raise ValueError("artifact location is bound to a different digest")
            if len(set(locations)) != len(locations):
                raise ValueError("artifact locations must be unique")
            resolved.append(ResolvedArtifact(artifact, tuple(sorted(locations))))
        return ResolvedDataset(manifest, tuple(resolved))
