# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical identity for Mindclade Python packages.

Layer 1 of `libs/python`: the standard library plus `libs.python.errors`. It owns
the text forms that cross a process or language boundary — content digests,
resource identifiers, resource versions, and artifact references — and nothing
about where any of them are stored.
"""

from .artifact import (
    ARTIFACT_REF_FIELDS,
    MAXIMUM_ARTIFACT_SIZE,
    MAXIMUM_LOGICAL_KIND_LENGTH,
    MAXIMUM_MEDIA_TYPE_LENGTH,
    MAXIMUM_SCHEMA_VERSION,
    ArtifactRef,
)
from .digest import (
    DIGEST_ALGORITHM,
    DIGEST_BINARY_SIZE,
    DIGEST_HEX_LENGTH,
    DIGEST_PREFIX,
    DIGEST_TEXT_LENGTH,
    Digest,
    is_canonical_digest,
)
from .resource import (
    COUNTER_SPACE,
    GUARANTEED_PER_MILLISECOND,
    ID_SEPARATOR,
    MAXIMUM_KIND_LENGTH,
    MINIMUM_KIND_LENGTH,
    UUID_BINARY_SIZE,
    UUID_COMPACT_LENGTH,
    UUID_VERSION,
    IdGenerator,
    ResourceId,
    is_canonical_kind,
    is_canonical_resource_id,
    new_resource_id,
    new_resource_id_at,
    parse_kind,
)
from .version import (
    MAXIMUM_GENERATION,
    MAXIMUM_RESOURCE_VERSION_LENGTH,
    SCHEMA_PREFIX,
    ResourceVersion,
    is_canonical_resource_version,
)

__all__ = [
    "ARTIFACT_REF_FIELDS",
    "COUNTER_SPACE",
    "DIGEST_ALGORITHM",
    "DIGEST_BINARY_SIZE",
    "DIGEST_HEX_LENGTH",
    "DIGEST_PREFIX",
    "DIGEST_TEXT_LENGTH",
    "GUARANTEED_PER_MILLISECOND",
    "ID_SEPARATOR",
    "MAXIMUM_ARTIFACT_SIZE",
    "MAXIMUM_GENERATION",
    "MAXIMUM_KIND_LENGTH",
    "MAXIMUM_LOGICAL_KIND_LENGTH",
    "MAXIMUM_MEDIA_TYPE_LENGTH",
    "MAXIMUM_RESOURCE_VERSION_LENGTH",
    "MAXIMUM_SCHEMA_VERSION",
    "MINIMUM_KIND_LENGTH",
    "SCHEMA_PREFIX",
    "UUID_BINARY_SIZE",
    "UUID_COMPACT_LENGTH",
    "UUID_VERSION",
    "ArtifactRef",
    "Digest",
    "IdGenerator",
    "ResourceId",
    "ResourceVersion",
    "is_canonical_digest",
    "is_canonical_kind",
    "is_canonical_resource_id",
    "is_canonical_resource_version",
    "new_resource_id",
    "new_resource_id_at",
    "parse_kind",
]
