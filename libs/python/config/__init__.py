# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Mindclade deterministic resolved-configuration contracts."""

from .fingerprint import canonical_json, fingerprint
from .loader import (
    MAXIMUM_OVERLAYS,
    MAXIMUM_OVERRIDES,
    MAXIMUM_SOURCE_BYTES,
    MAXIMUM_SOURCES,
    AppliedOverride,
    ResolvedConfig,
    Source,
    resolve,
)
from .merge import (
    MAXIMUM_MERGE_DEPTH,
    MAXIMUM_MERGE_LAYERS,
    MergeError,
    deep_merge,
    deep_merge_many,
)
from .overrides import MAXIMUM_OVERRIDE_LENGTH, OverrideError, apply_override
from .schema import RequiredField, get_path
from .validation import ValidationError, validate_required

__all__ = [
    "MAXIMUM_MERGE_DEPTH",
    "MAXIMUM_MERGE_LAYERS",
    "MAXIMUM_OVERLAYS",
    "MAXIMUM_OVERRIDES",
    "MAXIMUM_OVERRIDE_LENGTH",
    "MAXIMUM_SOURCES",
    "MAXIMUM_SOURCE_BYTES",
    "AppliedOverride",
    "MergeError",
    "OverrideError",
    "RequiredField",
    "ResolvedConfig",
    "Source",
    "ValidationError",
    "apply_override",
    "canonical_json",
    "deep_merge",
    "deep_merge_many",
    "fingerprint",
    "get_path",
    "resolve",
    "validate_required",
]
