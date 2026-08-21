# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable exact-version catalog view for training and evaluation consumers."""

from __future__ import annotations

from collections.abc import Iterable

from .versioning import DatasetVersionManifest


class DatasetCatalog:
    """Read-only projection; the durable Go registry remains authoritative."""

    def __init__(self, manifests: Iterable[DatasetVersionManifest]) -> None:
        entries: dict[tuple[str, str], DatasetVersionManifest] = {}
        digests: set[str] = set()
        for manifest in manifests:
            if not isinstance(manifest, DatasetVersionManifest):
                raise TypeError("catalog entries must be dataset manifests")
            key = (manifest.dataset_id, manifest.version)
            if key in entries or manifest.manifest_digest in digests:
                raise ValueError("dataset catalog identities must be unique")
            entries[key] = manifest
            digests.add(manifest.manifest_digest)
        if len(entries) > 1_000_000:
            raise ValueError("dataset catalog exceeds entry bound")
        self._entries = entries

    def resolve(self, dataset_id: str, version: str) -> DatasetVersionManifest:
        try:
            return self._entries[(dataset_id, version)]
        except KeyError as error:
            raise KeyError(f"unknown exact dataset version {dataset_id}@{version}") from error

    def versions(self, dataset_id: str) -> tuple[DatasetVersionManifest, ...]:
        return tuple(
            sorted(
                (item for (name, _), item in self._entries.items() if name == dataset_id),
                key=lambda item: tuple(int(part) for part in item.version.split(".")),
            )
        )
