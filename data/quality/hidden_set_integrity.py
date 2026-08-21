# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Metadata-only hidden-set access evidence validation."""

from __future__ import annotations

import re
from dataclasses import dataclass

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


@dataclass(frozen=True, slots=True)
class HiddenSetEvidence:
    manifest_digest: str
    access_policy_digest: str
    membership_digest: str
    enumerability_disabled: bool

    def __post_init__(self) -> None:
        for value in (self.manifest_digest, self.access_policy_digest, self.membership_digest):
            if not _DIGEST.fullmatch(value):
                raise ValueError("hidden-set evidence digest is invalid")
        if self.manifest_digest == self.membership_digest:
            raise ValueError("hidden membership must not be embedded in the public manifest")
        if self.enumerability_disabled is not True:
            raise ValueError("hidden-set identities must not be enumerable")
