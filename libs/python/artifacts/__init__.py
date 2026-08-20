# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded artifact identity, provenance, loading, and verification mechanisms."""

from .client import ArtifactReader, VerifiedArtifactClient
from .lineage import MAXIMUM_LINEAGE_EDGES, MAXIMUM_LINEAGE_NODES, lineage_order
from .manifest import (
    ARTIFACT_MANIFEST_FIELDS,
    ARTIFACT_MANIFEST_SCHEMA_VERSION,
    MAXIMUM_ANNOTATIONS,
    MAXIMUM_PARENTS,
    ArtifactManifest,
)
from .reference import MAXIMUM_IN_MEMORY_ARTIFACT_BYTES, reference_bytes
from .verification import MAXIMUM_CHUNKS, verify_bytes, verify_chunks

__all__ = [
    "ARTIFACT_MANIFEST_FIELDS",
    "ARTIFACT_MANIFEST_SCHEMA_VERSION",
    "MAXIMUM_ANNOTATIONS",
    "MAXIMUM_CHUNKS",
    "MAXIMUM_IN_MEMORY_ARTIFACT_BYTES",
    "MAXIMUM_LINEAGE_EDGES",
    "MAXIMUM_LINEAGE_NODES",
    "MAXIMUM_PARENTS",
    "ArtifactManifest",
    "ArtifactReader",
    "VerifiedArtifactClient",
    "lineage_order",
    "reference_bytes",
    "verify_bytes",
    "verify_chunks",
]
