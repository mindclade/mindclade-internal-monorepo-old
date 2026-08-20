# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical resolved-configuration serialization and digesting."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from libs.python.identifiers import Digest
from libs.python.serialization import canonical_json_bytes


def canonical_json(value: Mapping[str, Any]) -> bytes:
    """Compatibility name for the platform canonical JSON encoder."""
    return canonical_json_bytes(value)


def fingerprint(value: Mapping[str, Any]) -> str:
    """Return the canonical SHA-256 identity of a resolved document."""
    return Digest.of(canonical_json(value)).text
