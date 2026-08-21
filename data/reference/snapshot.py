# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reproducible reference database snapshot."""

from __future__ import annotations

import datetime as dt
import hashlib
import json
from dataclasses import dataclass

from .index import ReferenceIndex
from .source import ReferenceSource


@dataclass(frozen=True, slots=True)
class ReferenceSnapshot:
    reference_id: str
    version: str
    sources: tuple[ReferenceSource, ...]
    indexes: tuple[ReferenceIndex, ...]
    compatible_search_tools: tuple[str, ...]
    generated_at: dt.datetime
    build_provenance_digest: str

    def __post_init__(self) -> None:
        if not self.reference_id or len(self.reference_id) > 128 or not self.version:
            raise ValueError("reference snapshot identity/version is invalid")
        sources = tuple(sorted(self.sources))
        indexes = tuple(sorted(self.indexes))
        tools = tuple(sorted(self.compatible_search_tools))
        if not sources or not indexes or not tools:
            raise ValueError("reference snapshot requires sources, indexes, and compatible tools")
        if len({source.name for source in sources}) != len(sources):
            raise ValueError("reference snapshot source names must be unique")
        if len({(index.kind, index.format_version) for index in indexes}) != len(indexes):
            raise ValueError("reference snapshot indexes must be unique")
        if len(set(tools)) != len(tools) or any(not tool or len(tool) > 128 for tool in tools):
            raise ValueError("reference compatible tools are invalid")
        if self.generated_at.tzinfo is None or self.generated_at.utcoffset() is None:
            raise ValueError("reference generated_at must be timezone-aware")
        if len(self.build_provenance_digest) != 71 or not self.build_provenance_digest.startswith(
            "sha256:"
        ):
            raise ValueError("reference build provenance digest is invalid")
        object.__setattr__(self, "sources", sources)
        object.__setattr__(self, "indexes", indexes)
        object.__setattr__(self, "compatible_search_tools", tools)

    def canonical_document(self) -> str:
        value = {
            "schema_version": 1,
            "reference_id": self.reference_id,
            "version": self.version,
            "sources": [
                {
                    "name": source.name,
                    "release": source.release,
                    "snapshot_digest": source.snapshot_digest,
                    "uri": source.uri,
                    "cutoff": source.cutoff.astimezone(dt.UTC).isoformat(),
                    "license_ref": source.license_ref,
                }
                for source in self.sources
            ],
            "indexes": [
                {
                    "kind": index.kind,
                    "format_version": index.format_version,
                    "tool": index.tool,
                    "tool_version": index.tool_version,
                    "parameters_digest": index.parameters_digest,
                    "artifacts": [
                        {
                            "digest": artifact.digest,
                            "size_bytes": artifact.size_bytes,
                            "media_type": artifact.media_type,
                            "logical_kind": artifact.logical_kind,
                            "schema_version": artifact.schema_version,
                        }
                        for artifact in index.artifacts
                    ],
                }
                for index in self.indexes
            ],
            "compatible_search_tools": list(self.compatible_search_tools),
            "generated_at": self.generated_at.astimezone(dt.UTC).isoformat(),
            "build_provenance_digest": self.build_provenance_digest,
        }
        return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"

    @property
    def digest(self) -> str:
        return "sha256:" + hashlib.sha256(self.canonical_document().encode()).hexdigest()
