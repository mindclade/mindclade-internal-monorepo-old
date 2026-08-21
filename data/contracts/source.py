# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable source-snapshot identity and use constraints."""

from __future__ import annotations

import datetime as dt
import re
from dataclasses import dataclass
from urllib.parse import urlsplit

from .record import Sensitivity

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_OWNER = re.compile(r"[a-z][a-z0-9-]{1,62}")


@dataclass(frozen=True)
class SourceSnapshot:
    """One content-addressed upstream snapshot.

    URI values identify an approved object or HTTPS boundary. Credentials and signed query
    strings are rejected; authentication belongs to the execution identity.
    """

    uri: str
    digest: str
    captured_at: dt.datetime
    owner: str
    classification: Sensitivity
    license_ref: str
    use_constraints: tuple[str, ...]

    def __post_init__(self) -> None:
        parsed = urlsplit(self.uri) if isinstance(self.uri, str) else None
        if (
            parsed is None
            or parsed.scheme not in {"gs", "s3", "https"}
            or not parsed.hostname
            or not parsed.path.strip("/")
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("source uri must be an unsigned gs://, s3://, or https:// URI")
        if not isinstance(self.digest, str) or not _DIGEST.fullmatch(self.digest):
            raise ValueError("source digest must be canonical SHA-256")
        if (
            not isinstance(self.captured_at, dt.datetime)
            or self.captured_at.tzinfo is None
            or self.captured_at.utcoffset() is None
        ):
            raise ValueError("captured_at must be timezone-aware")
        if not isinstance(self.owner, str) or not _OWNER.fullmatch(self.owner):
            raise ValueError("source owner must be a bounded team identifier")
        if not isinstance(self.classification, Sensitivity):
            raise ValueError("classification must be a Sensitivity")
        if (
            not isinstance(self.license_ref, str)
            or not self.license_ref.strip()
            or len(self.license_ref) > 256
        ):
            raise ValueError("license_ref must be non-empty and bounded")
        constraints = tuple(self.use_constraints)
        if any(
            not isinstance(item, str) or not item.strip() or len(item) > 256 for item in constraints
        ):
            raise ValueError("use constraints must be non-empty and bounded")
        if not constraints or len(constraints) > 32 or len(set(constraints)) != len(constraints):
            raise ValueError("1..32 unique use constraints are required")
        object.__setattr__(self, "use_constraints", constraints)
