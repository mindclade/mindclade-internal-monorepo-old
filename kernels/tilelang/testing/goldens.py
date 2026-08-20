# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Minimal failure identity safe for logs and retained evidence indexes."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class FailureIdentity:
    seed: int
    request_digest: str
    input_digest: str
    generated_source_digest: str | None


def tensor_bytes_digest(payload: bytes) -> str:
    return hashlib.sha256(payload).hexdigest()
