# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable upstream reference-source identity."""

from __future__ import annotations

import datetime as dt
import re
from dataclasses import dataclass
from urllib.parse import urlsplit

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_NAME = re.compile(r"[a-z][a-z0-9._-]{0,127}")


@dataclass(frozen=True, slots=True, order=True)
class ReferenceSource:
    name: str
    release: str
    snapshot_digest: str
    uri: str
    cutoff: dt.datetime
    license_ref: str

    def __post_init__(self) -> None:
        if not _NAME.fullmatch(self.name) or not self.release or len(self.release) > 128:
            raise ValueError("reference source name/release is invalid")
        if not _DIGEST.fullmatch(self.snapshot_digest):
            raise ValueError("reference source snapshot digest is invalid")
        parsed = urlsplit(self.uri)
        if (
            parsed.scheme not in {"gs", "s3", "https"}
            or not parsed.hostname
            or not parsed.path.strip("/")
            or parsed.query
            or parsed.fragment
            or parsed.username is not None
        ):
            raise ValueError("reference source URI must be absolute and credential-free")
        if self.cutoff.tzinfo is None or self.cutoff.utcoffset() is None:
            raise ValueError("reference source cutoff must be timezone-aware")
        if not self.license_ref or len(self.license_ref) > 256:
            raise ValueError("reference source license is invalid")
