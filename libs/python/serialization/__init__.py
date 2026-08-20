# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical byte encodings for Mindclade Python packages.

Layer 1 of `libs/python`: the standard library plus `libs.python.errors`. It owns
the question "what bytes represent this document", and deliberately not "what is
this document's identity" — digesting belongs to `libs.python.identifiers`.
"""

from .canonical import (
    FIELD_SEPARATOR,
    LINE_SEPARATOR,
    canonical_field,
    canonical_json_bytes,
    canonical_lines,
)

__all__ = [
    "FIELD_SEPARATOR",
    "LINE_SEPARATOR",
    "canonical_field",
    "canonical_json_bytes",
    "canonical_lines",
]
