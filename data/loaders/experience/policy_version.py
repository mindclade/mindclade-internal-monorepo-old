# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable policy identity for rollout trajectories."""

from __future__ import annotations

import re
from dataclasses import dataclass

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


@dataclass(frozen=True, slots=True)
class PolicyVersion:
    model_digest: str
    runtime_digest: str
    configuration_digest: str

    def __post_init__(self) -> None:
        if any(
            not isinstance(value, str) or not _DIGEST.fullmatch(value)
            for value in (self.model_digest, self.runtime_digest, self.configuration_digest)
        ):
            raise ValueError("policy version digests are invalid")
